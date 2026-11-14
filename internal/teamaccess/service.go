package teamaccess

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
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
	role    string
	teamURL string
	brokers []string
	topic   string
}

func New(ctx context.Context, db *pgxpool.Pool) (*Service, error) {
	role := os.Getenv("TEAM_ACCESS_ROLE")
	if role == "" {
		role = "team"
	}
	if role != "team" && role != "rights" {
		return nil, fmt.Errorf("invalid TEAM_ACCESS_ROLE %q", role)
	}
	migration := os.Getenv("TEAM_ACCESS_MIGRATION")
	if migration == "" {
		migration = "migrations/team_access/001_" + role + ".sql"
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
	s := &Service{DB: db, key: key, issuer: "team-access", role: role, teamURL: os.Getenv("TEAM_DIRECTORY_URL"), brokers: strings.Split(os.Getenv("KAFKA_BROKERS"), ","), topic: "team-access-events"}
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
	if s.role == "rights" {
		path := os.Getenv("TEAM_ACCESS_SEED_RESOURCES_FILE")
		if path == "" {
			return nil
		}
		resources, err := loadSeedResources(path)
		if err != nil {
			return err
		}
		for _, resource := range resources {
			_, err = s.DB.Exec(ctx, `INSERT INTO ta_resources(id,team_id,kind,name,external_ref,url,capabilities,approver_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO UPDATE SET team_id=EXCLUDED.team_id,kind=EXCLUDED.kind,name=EXCLUDED.name,external_ref=EXCLUDED.external_ref,url=EXCLUDED.url,capabilities=EXCLUDED.capabilities,approver_id=EXCLUDED.approver_id`, resource.ID, resource.TeamID, resource.Kind, resource.Name, resource.ExternalRef, resource.URL, resource.Capabilities, resource.ApproverID)
			if err != nil {
				return err
			}
		}
		return nil
	}
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
	var err error
	directory := []struct{ id, username, name string }{
		{"catalog-lead", "catalog.lead", "Алексей Морозов"}, {"catalog-mentor", "catalog.mentor", "Вера Соколова"}, {"catalog-dev", "catalog.dev", "Ирина Белова"},
		{"search-lead", "search.lead", "Павел Орлов"}, {"search-mentor", "search.mentor", "Мария Котова"}, {"search-analyst", "search.analyst", "Денис Романов"},
		{"platform-lead", "platform.lead", "Ольга Крылова"}, {"platform-mentor", "platform.mentor", "Сергей Лебедев"}, {"platform-dev", "platform.dev", "Наталья Волкова"},
	}
	for _, u := range directory {
		// These are directory records, not provisioned login accounts.
		if _, err = s.DB.Exec(ctx, `INSERT INTO ta_users(id,username,display_name,password_hash) VALUES($1,$2,$3,'!') ON CONFLICT(id) DO NOTHING`, u.id, u.username, u.name); err != nil {
			return err
		}
	}
	teams := []struct{ id, name, mentor, lead string }{
		{"demo-team", "Учебная команда", "approver", "approver"},
		{"catalog-team", "Каталог и продавец", "catalog-mentor", "catalog-lead"},
		{"search-team", "Поиск и рекомендации", "search-mentor", "search-lead"},
		{"platform-team", "Платформа разработки", "platform-mentor", "platform-lead"},
	}
	for _, team := range teams {
		if _, err = s.DB.Exec(ctx, `INSERT INTO ta_teams(id,name,mentor_id,lead_id) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO NOTHING`, team.id, team.name, team.mentor, team.lead); err != nil {
			return err
		}
	}
	memberships := []struct{ user, team, role string }{
		{"intern", "demo-team", "intern"}, {"intern2", "demo-team", "intern"}, {"approver", "demo-team", "approver"},
		{"catalog-lead", "catalog-team", "lead"}, {"catalog-mentor", "catalog-team", "mentor"}, {"catalog-dev", "catalog-team", "developer"}, {"approver", "catalog-team", "approver"},
		{"search-lead", "search-team", "lead"}, {"search-mentor", "search-team", "mentor"}, {"search-analyst", "search-team", "analyst"}, {"intern2", "search-team", "intern"}, {"approver", "search-team", "approver"},
		{"platform-lead", "platform-team", "lead"}, {"platform-mentor", "platform-team", "mentor"}, {"platform-dev", "platform-team", "developer"}, {"approver", "platform-team", "approver"},
	}
	for _, membership := range memberships {
		if _, err = s.DB.Exec(ctx, `INSERT INTO ta_memberships(user_id,team_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, membership.user, membership.team, membership.role); err != nil {
			return err
		}
	}
	// The directory owns Jira identity metadata; credentials stay in the Jira adapter.
	var realAccounts map[string]string
	if raw := os.Getenv("TEAM_JIRA_ACCOUNTS_JSON"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &realAccounts); err != nil {
			return fmt.Errorf("TEAM_JIRA_ACCOUNTS_JSON: %w", err)
		}
	}
	base := strings.TrimRight(os.Getenv("JIRA_BASE_URL"), "/")
	validBase := strings.HasPrefix(base, "https://") || (os.Getenv("JIRA_MODE") == "mock" && strings.HasPrefix(base, "http://"))
	for _, id := range []string{"intern", "intern2", "approver", "catalog-lead", "catalog-mentor", "catalog-dev", "search-lead", "search-mentor", "search-analyst", "platform-lead", "platform-mentor", "platform-dev"} {
		account, ok := realAccounts[id]
		profile := "https://jira.example.invalid/jira/people/" + url.PathEscape(id)
		if !ok {
			account = "demo-" + id
		} else {
			if account == "" || !validBase {
				return fmt.Errorf("real Jira account for %s requires an account ID and a valid JIRA_BASE_URL", id)
			}
			profile = base + "/jira/people/" + url.PathEscape(account)
		}
		if _, err = s.DB.Exec(ctx, `UPDATE ta_users SET jira_account_id=$2,jira_profile_url=$3 WHERE id=$1 AND (jira_account_id IS NULL OR $4)`, id, account, profile, ok); err != nil {
			return err
		}
	}
	return nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func now() time.Time { return time.Now().UTC() }
