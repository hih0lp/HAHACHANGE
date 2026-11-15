package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hackchange/avito-rag/internal/teamaccess"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(context.Background(), (*http.Server).ListenAndServe); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, serve func(*http.Server) error) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		return err
	}
	s, err := teamaccess.New(ctx, db)
	if err != nil {
		return err
	}
	if os.Getenv("TEAM_ACCESS_ROLE") == "rights" {
		go s.PublishOutbox(ctx)
	}
	addr := os.Getenv("TEAM_ACCESS_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	handler := s.Routes()
	if os.Getenv("TEAM_ACCESS_ROLE") == "rights" {
		mux := http.NewServeMux()
		mux.Handle("/mcp", newRightsMCP(false))
		mux.Handle("/mcp/confirm", newRightsMCP(true))
		mux.Handle("/", handler)
		handler = mux
	} else {
		selfURL := os.Getenv("TEAM_SELF_URL")
		if selfURL == "" {
			selfURL = "http://127.0.0.1:8080"
		}
		mux := http.NewServeMux()
		mux.Handle("/mcp", newTeamMCP(selfURL))
		mux.Handle("/", handler)
		handler = mux
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("team-access listening on %s", addr)
	return serve(server)
}
