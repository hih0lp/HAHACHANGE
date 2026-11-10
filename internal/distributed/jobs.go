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
	ID       string          `json:"id"`
	Query    string          `json:"query"`
	Scenario string          `json:"scenario"`
	Status   string          `json:"status"`
	Answer   string          `json:"answer,omitempty"`
	Sources  json.RawMessage `json:"sources,omitempty"`
	Error    string          `json:"error,omitempty"`
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
	id, err := NewID()
	if err != nil {
		return Job{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Job{}, err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO ai_jobs(id,owner_id,scenario,query,status) VALUES($1,$2,$3,$4,'queued')`, id, owner, scenario, query)
	if err != nil {
		return Job{}, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO ai_job_outbox(job_id,event_type) VALUES($1,'requested')`, id)
	if err != nil {
		return Job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, err
	}
	job := Job{ID: id, Query: query, Scenario: scenario, Status: "queued"}
	s.cache(ctx, job)
	return job, nil
}

func (s *System) Get(ctx context.Context, owner, id string) (Job, error) {
	var job Job
	var sources []byte
	err := s.DB.QueryRow(ctx, `SELECT id::text, query, scenario, status, COALESCE(answer,''), sources, COALESCE(error,'') FROM ai_jobs WHERE id=$1 AND owner_id=$2`, id, owner).
		Scan(&job.ID, &job.Query, &job.Scenario, &job.Status, &job.Answer, &sources, &job.Error)
	if err != nil {
		return Job{}, err
	}
	job.Sources = json.RawMessage(sources)
	if s.Redis != nil && (job.Status == "queued" || job.Status == "running") {
		if cached, err := s.Redis.Get(ctx, "ai-job:"+id).Bytes(); err == nil {
			var quick struct{ Status string `json:"status"` }
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
	type event struct{ row int64; job string }
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

func (s *System) ProcessOne(ctx context.Context, id string, answer AnswerFunc, timeout time.Duration) error {
	lease, err := NewID()
	if err != nil {
		return err
	}
	var query string
	var tries int
	err = s.DB.QueryRow(ctx, `UPDATE ai_jobs SET status='running', lease_token=$2, lease_until=now()+$3::interval,
		attempt=attempt+1, updated_at=now() WHERE id=$1 AND status='queued' RETURNING query, attempt`, id, lease, fmt.Sprintf("%d seconds", int(timeout.Seconds())+30)).Scan(&query, &tries)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // Already running or terminal; duplicate Kafka delivery.
	}
	if err != nil {
		return err
	}
	s.cache(ctx, Job{ID: id, Status: "running"})
	workCtx, cancel := context.WithTimeout(ctx, timeout)
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
