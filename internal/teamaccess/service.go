package teamaccess

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	DB      *pgxpool.Pool
	key     *rsa.PrivateKey
	issuer  string
	brokers []string
	topic   string
}

func New(ctx context.Context, db *pgxpool.Pool) (*Service, error) {
	migration := os.Getenv("TEAM_ACCESS_MIGRATION")
	if migration == "" {
		migration = "migrations/team_access/001_schema.sql"
	}
	sql, err := os.ReadFile(migration)
	if err != nil {
		return nil, err
	}
	if _, err = db.Exec(ctx, string(sql)); err != nil {
		return nil, fmt.Errorf("team access migration: %w", err)
	}
	key, err := loadKey(os.Getenv("TEAM_ACCESS_KEY_FILE"))
	if err != nil {
		return nil, err
	}
	s := &Service{DB: db, key: key, issuer: "team-access", brokers: strings.Split(os.Getenv("KAFKA_BROKERS"), ","), topic: "team-access-events"}
	if v := os.Getenv("TEAM_ACCESS_ISSUER"); v != "" {
		s.issuer = v
	}
	if err = s.seed(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func loadKey(path string) (*rsa.PrivateKey, error) {
	if path == "" {
		return nil, errors.New("TEAM_ACCESS_KEY_FILE is required")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, e := rsa.GenerateKey(rand.Reader, 2048)
		if e != nil {
			return nil, e
		}
		b := x509.MarshalPKCS1PrivateKey(key)
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return nil, e
		}
		if e = os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: b}), 0600); e != nil {
			return nil, e
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid RSA key")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}

func (s *Service) seed(ctx context.Context) error {
	// Demo identities are explicit. Passwords are supplied by the operator, never committed.
	seeds := []struct{ id, username, name, role, passwordEnv string }{
		{"intern", "intern", "Стажёр", "intern", "TEAM_ACCESS_INTERN_PASSWORD"},
		{"intern2", "intern2", "Второй стажёр", "intern", "TEAM_ACCESS_INTERN2_PASSWORD"},
		{"approver", "approver", "Наставник", "approver", "TEAM_ACCESS_APPROVER_PASSWORD"},
	}
	for _, u := range seeds {
		pass := os.Getenv(u.passwordEnv)
		if len(pass) < 12 {
			return fmt.Errorf("%s must have at least 12 characters", u.passwordEnv)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		// Existing users keep their password across restarts unless explicitly changed in the DB.
		_, err = s.DB.Exec(ctx, `INSERT INTO ta_users(id,username,display_name,password_hash) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, u.id, u.username, u.name, string(hash))
		if err != nil {
			return err
		}
	}
	_, err := s.DB.Exec(ctx, `INSERT INTO ta_teams(id,name,mentor_id,lead_id) VALUES('demo-team','Учебная команда','approver','approver') ON CONFLICT(id) DO NOTHING`)
	if err != nil {
		return err
	}
	for _, u := range seeds {
		if _, err = s.DB.Exec(ctx, `INSERT INTO ta_memberships(user_id,team_id,role) VALUES($1,'demo-team',$2) ON CONFLICT DO NOTHING`, u.id, u.role); err != nil {
			return err
		}
	}
	jiraURL := os.Getenv("TEAM_ACCESS_SEED_JIRA_URL")
	if jiraURL != "" {
		_, err = s.DB.Exec(ctx, `INSERT INTO ta_resources(id,team_id,kind,name,external_ref,url,capabilities,approver_id)
   VALUES('demo-jira','demo-team','jira_project','Учебный Jira-проект',$1,$2,ARRAY['browse_issues'],'approver') ON CONFLICT(id) DO NOTHING`, os.Getenv("JIRA_PROJECT_KEY"), jiraURL)
	}
	return err
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func now() time.Time { return time.Now().UTC() }
