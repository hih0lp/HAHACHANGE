package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/hackchange/avito-rag/internal/teamaccess"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err = db.Ping(ctx); err != nil {
		log.Fatal(err)
	}
	s, err := teamaccess.New(ctx, db)
	if err != nil {
		log.Fatal(err)
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
	}
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("team-access listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
