package mockfixture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/hackchange/avito-rag/internal/scenario"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Dataset stores a versioned snapshot shared by REST and MCP readers.
type Dataset struct {
	db       *pgxpool.Pool
	name     string
	validate func([]byte) error
}

type snapshot struct {
	Version int64           `json:"version"`
	Data    json.RawMessage `json:"data"`
}

var ErrItemNotFound = errors.New("mock item not found")

// RegisterMutation applies one validated edit while holding the dataset row lock.
func (d *Dataset) RegisterMutation(mux *http.ServeMux, directory scenario.Client, pattern string, mutate func([]byte, *http.Request, []byte) ([]byte, error)) {
	mux.HandleFunc("PUT "+pattern, func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			datasetError(w, 401, "Откройте демосессию наставника.")
			return
		}
		profile, err := directory.Profile(r.Context(), token)
		if err != nil || profile.ID == "" {
			datasetError(w, 403, "Требуется действительная сессия пользователя.")
			return
		}
		body := readBody(w, r)
		if body == nil {
			datasetError(w, 400, "Некорректный или слишком большой запрос.")
			return
		}
		tx, err := d.db.Begin(r.Context())
		if err != nil {
			d.failed(w, err)
			return
		}
		defer tx.Rollback(r.Context())
		var current snapshot
		err = tx.QueryRow(r.Context(), "SELECT version,data FROM bobby_mock_datasets WHERE name=$1 FOR UPDATE", d.name).Scan(&current.Version, &current.Data)
		if err != nil {
			d.failed(w, err)
			return
		}
		changed, err := mutate(current.Data, r, body)
		if errors.Is(err, ErrItemNotFound) {
			datasetError(w, 404, "Запись не найдена.")
			return
		}
		if err == nil {
			err = d.validate(changed)
		}
		if err != nil {
			datasetError(w, 400, err.Error())
			return
		}
		if err = tx.QueryRow(r.Context(), "UPDATE bobby_mock_datasets SET version=version+1,data=$2 WHERE name=$1 RETURNING version,data", d.name, changed).Scan(&current.Version, &current.Data); err != nil {
			d.failed(w, err)
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			d.failed(w, err)
			return
		}
		datasetJSON(w, 200, map[string]any{"version": current.Version, "saved": true})
	})
}

func OpenDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	if url == "" {
		return nil, nil
	}
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err = createTable(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func createTable(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(932860491)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS bobby_mock_datasets (
		name text PRIMARY KEY,
		version bigint NOT NULL DEFAULT 1,
		data jsonb NOT NULL
	)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Seed only a missing dataset. Restarting a mock never replaces saved edits.
func Seed(ctx context.Context, db *pgxpool.Pool, name string, initial func() ([]byte, error), validate func([]byte) error) (*Dataset, error) {
	if db == nil {
		return nil, nil
	}
	d := &Dataset{db: db, name: name, validate: validate}
	if _, err := d.read(ctx); err == nil {
		return d, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	data, err := initial()
	if err == nil {
		err = validate(data)
	}
	if err != nil {
		return nil, fmt.Errorf("seed %s: %w", name, err)
	}
	_, err = db.Exec(ctx, "INSERT INTO bobby_mock_datasets(name, data) VALUES($1, $2) ON CONFLICT DO NOTHING", name, data)
	return d, err
}

func (d *Dataset) read(ctx context.Context) (snapshot, error) {
	var out snapshot
	err := d.db.QueryRow(ctx, "SELECT version, data FROM bobby_mock_datasets WHERE name=$1", d.name).Scan(&out.Version, &out.Data)
	return out, err
}

func (d *Dataset) Bytes(ctx context.Context) ([]byte, error) {
	out, err := d.read(ctx)
	return out.Data, err
}

func (d *Dataset) Register(mux *http.ServeMux, directory scenario.Client) {
	path := "/v1/mock-data/" + d.name
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodPut {
			w.Header().Set("Allow", "GET, PUT")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			datasetError(w, 401, "Откройте демосессию наставника.")
			return
		}
		profile, err := directory.Profile(r.Context(), token)
		if err != nil || profile.ID != "approver" {
			datasetError(w, 403, "Редактировать учебные данные может наставник.")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet {
			out, err := d.read(r.Context())
			if err != nil {
				d.failed(w, err)
				return
			}
			datasetJSON(w, 200, out)
			return
		}
		var in snapshot
		if err := json.Unmarshal(readBody(w, r), &in); err != nil || in.Version < 1 || len(in.Data) == 0 {
			datasetError(w, 400, "Требуются данные и версия загруженного списка.")
			return
		}
		if err := d.validate(in.Data); err != nil {
			datasetError(w, 400, err.Error())
			return
		}
		var out snapshot
		err = d.db.QueryRow(r.Context(), `UPDATE bobby_mock_datasets SET version=version+1, data=$1
			WHERE name=$2 AND version=$3 RETURNING version, data`, in.Data, d.name, in.Version).Scan(&out.Version, &out.Data)
		if errors.Is(err, pgx.ErrNoRows) {
			datasetError(w, 409, "Данные уже изменились. Обновите список перед сохранением.")
			return
		}
		if err != nil {
			d.failed(w, err)
			return
		}
		datasetJSON(w, 200, out)
	})
}

func (d *Dataset) failed(w http.ResponseWriter, err error) {
	log.Printf("mock dataset %s: %v", d.name, err)
	datasetError(w, 503, "Учебный сервис временно недоступен.")
}

func readBody(w http.ResponseWriter, r *http.Request) []byte {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		return nil
	}
	return data
}

func datasetError(w http.ResponseWriter, code int, message string) {
	datasetJSON(w, code, map[string]string{"error": message})
}

func datasetJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
