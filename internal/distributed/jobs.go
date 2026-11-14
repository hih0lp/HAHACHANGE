package distributed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

type Job struct {
	ID          string          `json:"id"`
	ThreadID    string          `json:"thread_id,omitempty"`
	Query       string          `json:"query"`
	Scenario    string          `json:"scenario"`
	Status      string          `json:"status"`
	Answer      string          `json:"answer,omitempty"`
	Sources     json.RawMessage `json:"sources,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type System struct {
	DB       *pgxpool.Pool
	Redis    *redis.Client
	Writer   *kafka.Writer
	Reader   *kafka.Reader
	Topic    string
	MaxTries int
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex.EncodeToString(b[:4]), hex.EncodeToString(b[4:6]), hex.EncodeToString(b[6:8]), hex.EncodeToString(b[8:10]), hex.EncodeToString(b[10:])), nil
}

func (s *System) Submit(ctx context.Context, owner, scenario, query string) (Job, error) {
	return s.SubmitWithContext(ctx, owner, scenario, query, nil)
}

func (s *System) SubmitWithContext(ctx context.Context, owner, scenario, query string, teamContext json.RawMessage) (Job, error) {
	return s.submit(ctx, owner, scenario, query, "", teamContext, "")
}

func (s *System) SubmitChat(ctx context.Context, owner, scenario, query, thread, token string) (Job, error) {
	return s.submit(ctx, owner, scenario, query, thread, nil, token)
}

func (s *System) submit(ctx context.Context, owner, scenario, query, thread string, teamContext json.RawMessage, token string) (Job, error) {
	id, err := NewID()
	if err != nil {
		return Job{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	switch thread {
	case "new":
		thread = id
		_, err = tx.Exec(ctx, `INSERT INTO ai_chat_threads(id, owner_id, title) VALUES($1,$2,left($3,80))`, thread, owner, query)
	case "", "legacy":
		thread = ""
	default:
		err = tx.QueryRow(ctx, `UPDATE ai_chat_threads SET updated_at=GREATEST(updated_at,now())
			WHERE id=$1 AND owner_id=$2 RETURNING id::text`, thread, owner).Scan(&thread)
	}
	if err != nil {
		return Job{}, err
	}
	var createdAt time.Time
	err = tx.QueryRow(ctx, `INSERT INTO ai_jobs(id,owner_id,scenario,query,status,team_context,thread_id)
		VALUES($1,$2,$3,$4,'queued',$5,NULLIF($6,'')::uuid) RETURNING created_at`, id, owner, scenario, query, teamContext, thread).Scan(&createdAt)
	if err != nil {
		return Job{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ai_job_outbox(job_id,event_type) VALUES($1,'requested')`, id)
	if err != nil {
		return Job{}, err
	}
	if err := s.StoreTeamToken(ctx, id, token); err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	job := Job{ID: id, ThreadID: thread, Query: query, Scenario: scenario, Status: "queued", CreatedAt: createdAt}
	s.cache(ctx, job)
	return job, nil
}

func (s *System) Get(ctx context.Context, owner, id string) (Job, error) {
	var job Job
	var sources []byte
	err := s.DB.QueryRow(ctx, `SELECT id::text, COALESCE(thread_id::text,''), query, scenario, status, COALESCE(answer,''), sources, COALESCE(error,''), created_at, completed_at FROM ai_jobs WHERE id=$1 AND owner_id=$2`, id, owner).
		Scan(&job.ID, &job.ThreadID, &job.Query, &job.Scenario, &job.Status, &job.Answer, &sources, &job.Error, &job.CreatedAt, &job.CompletedAt)
	if err != nil {
		return Job{}, err
	}
	job.Sources = json.RawMessage(sources)
	if s.Redis != nil && (job.Status == "queued" || job.Status == "running") {
		if cached, err := s.Redis.Get(ctx, "ai-job:"+id).Bytes(); err == nil {
			var quick struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(cached, &quick) == nil && quick.Status == "running" {
				job.Status = "running"
			}
		}
	}
	return job, nil
}

func (s *System) cache(ctx context.Context, job Job) {
	if s.Redis == nil {
		return
	}
	data, err := json.Marshal(struct {
		Status string `json:"status"`
	}{job.Status})
	if err == nil {
		_ = s.Redis.Set(ctx, "ai-job:"+job.ID, data, 10*time.Minute).Err()
	}
}

// PublishPending is safe to run on multiple API instances. A crash between
// Kafka acknowledgement and marking the row published can produce a duplicate;
// workers claim jobs by ID and complete them conditionally.
func (s *System) PublishPending(ctx context.Context) error {
	if s.Writer == nil {
		return errors.New("Kafka writer is not configured")
	}
	rows, err := s.DB.Query(ctx, `SELECT id, job_id::text FROM ai_job_outbox WHERE published_at IS NULL ORDER BY id LIMIT 32`)
	if err != nil {
		return err
	}
	type event struct {
		row int64
		job string
	}
	var pending []event
	for rows.Next() {
		var item event
		if err = rows.Scan(&item.row, &item.job); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, item := range pending {
		if err = s.Writer.WriteMessages(ctx, kafka.Message{Key: []byte(item.job), Value: []byte(item.job)}); err != nil {
			return err
		}
		if _, err = s.DB.Exec(ctx, `UPDATE ai_job_outbox SET published_at=now() WHERE id=$1 AND published_at IS NULL`, item.row); err != nil {
			return err
		}
	}
	return nil
}

// RequeueExpired restores work when a worker disappears after claiming a job.
func (s *System) RequeueExpired(ctx context.Context) error {
	_, err := s.DB.Exec(ctx, `WITH expired AS (
		UPDATE ai_jobs SET status='queued', lease_token=NULL, lease_until=NULL, updated_at=now()
		WHERE status='running' AND lease_until < now() RETURNING id
	) UPDATE ai_job_outbox AS o SET published_at=NULL FROM expired WHERE o.job_id=expired.id AND o.event_type='requested'`)
	return err
}

type AnswerFunc func(context.Context, string) (string, any, error)

type teamContextKey struct{}
type teamTokenKey struct{}
type chatJobKey struct{}

// ChatJob identifies the owner and current turn for loading earlier completed
// turns. It is set by the worker from the claimed job, not from model input.
func ChatJob(ctx context.Context) (owner, jobID string) {
	v, _ := ctx.Value(chatJobKey{}).([2]string)
	return v[0], v[1]
}

func TeamContext(ctx context.Context) string {
	v, _ := ctx.Value(teamContextKey{}).(string)
	return v
}

// TeamToken is kept only in Redis for the lifetime of a queued job. It is
// deliberately never written to PostgreSQL, Kafka or an LLM prompt.
func TeamToken(ctx context.Context) string {
	v, _ := ctx.Value(teamTokenKey{}).(string)
	return v
}

// WithTeamToken carries the authenticated team session to the answerer.
func WithTeamToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, teamTokenKey{}, token)
}

func (s *System) StoreTeamToken(ctx context.Context, jobID, token string) error {
	if token == "" {
		return nil
	}
	if s.Redis == nil {
		return errors.New("redis is required for team-access tools")
	}
	return s.Redis.Set(ctx, "ai-job-team-token:"+jobID, token, 15*time.Minute).Err()
}

func (s *System) teamToken(ctx context.Context, jobID string) string {
	if s.Redis == nil {
		return ""
	}
	token, err := s.Redis.Get(ctx, "ai-job-team-token:"+jobID).Result()
	if err != nil {
		return ""
	}
	return token
}

func (s *System) clearTeamToken(ctx context.Context, jobID string) {
	if s.Redis != nil {
		_ = s.Redis.Del(ctx, "ai-job-team-token:"+jobID).Err()
	}
}

func (s *System) ProcessOne(ctx context.Context, id string, answer AnswerFunc, timeout time.Duration) error {
	lease, err := NewID()
	if err != nil {
		return err
	}
	var query, owner string
	var teamContext []byte
	var tries int
	err = s.DB.QueryRow(ctx, `UPDATE ai_jobs SET status='running', lease_token=$2, lease_until=now()+$3::interval,
		attempt=attempt+1, updated_at=now() WHERE id=$1 AND status='queued' RETURNING query, owner_id, COALESCE(team_context,'null'::jsonb), attempt`, id, lease, fmt.Sprintf("%d seconds", int(timeout.Seconds())+30)).Scan(&query, &owner, &teamContext, &tries)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // Already running or terminal; duplicate Kafka delivery.
	}
	if err != nil {
		return err
	}
	s.cache(ctx, Job{ID: id, Status: "running"})
	workCtx, cancel := context.WithTimeout(ctx, timeout)
	workCtx = context.WithValue(workCtx, teamContextKey{}, string(teamContext))
	workCtx = WithTeamToken(workCtx, s.teamToken(ctx, id))
	workCtx = context.WithValue(workCtx, chatJobKey{}, [2]string{owner, id})
	answerText, sources, workErr := answer(workCtx, query)
	cancel()
	if workErr != nil {
		if tries < s.MaxTries {
			_, err = s.DB.Exec(ctx, `WITH reset AS (
				UPDATE ai_jobs SET status='queued', lease_token=NULL, lease_until=NULL, updated_at=now()
				WHERE id=$1 AND lease_token=$2 AND status='running' RETURNING id
			) UPDATE ai_job_outbox AS o SET published_at=NULL FROM reset WHERE o.job_id=reset.id AND o.event_type='requested'`, id, lease)
			s.cache(ctx, Job{ID: id, Status: "queued"})
			return err
		}
		_, err = s.DB.Exec(ctx, `UPDATE ai_jobs SET status='failed', error=$3, lease_token=NULL, lease_until=NULL,
			updated_at=now(), completed_at=now() WHERE id=$1 AND lease_token=$2 AND status='running'`, id, lease, "Ответ временно недоступен. Повторите запрос позже.")
		s.cache(ctx, Job{ID: id, Status: "failed"})
		return err
	}
	encoded, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `UPDATE ai_jobs SET status='completed', answer=$3, sources=$4,
		lease_token=NULL, lease_until=NULL, updated_at=now(), completed_at=now()
		WHERE id=$1 AND lease_token=$2 AND status='running'`, id, lease, answerText, encoded)
	if err == nil {
		s.cache(ctx, Job{ID: id, Status: "completed"})
		s.clearTeamToken(ctx, id)
	}
	return err
}

func (s *System) Work(ctx context.Context, answer AnswerFunc, timeout time.Duration) error {
	if s.Reader == nil {
		return errors.New("Kafka reader is not configured")
	}
	for {
		message, err := s.Reader.FetchMessage(ctx)
		if err != nil {
			return err
		}
		if err := s.ProcessOne(ctx, string(message.Value), answer, timeout); err != nil {
			return err
		}
		if err := s.Reader.CommitMessages(ctx, message); err != nil {
			return err
		}
	}
}
