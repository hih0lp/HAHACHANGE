package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hackchange/avito-rag/internal/distributed"
	"github.com/hackchange/avito-rag/internal/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

func runDistributed(cfg config) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if os.Getenv("DATABASE_URL") == "" || os.Getenv("SESSION_SECRET") == "" {
		return fmt.Errorf("distributed mode requires DATABASE_URL and SESSION_SECRET")
	}
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("PostgreSQL: %w", err)
	}
	var schemaReady bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.ai_jobs') IS NOT NULL`).Scan(&schemaReady); err != nil {
		return fmt.Errorf("job schema: %w", err)
	}
	if !schemaReady {
		return fmt.Errorf("job schema is missing: apply migrations/001_distributed_jobs.sql")
	}
	// Idempotent migration also handles already-initialized PostgreSQL volumes.
	if _, err := pool.Exec(ctx, `ALTER TABLE ai_jobs ADD COLUMN IF NOT EXISTS team_context jsonb`); err != nil {
		return fmt.Errorf("team context schema: %w", err)
	}
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS ai_scenario_events (event_id text PRIMARY KEY, received_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("scenario events schema: %w", err)
	}
	if os.Getenv("APP_ROLE") != "worker" && os.Getenv("TEAM_ACCESS_URL") != "" {
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.ai_scenarios') IS NOT NULL`).Scan(&schemaReady); err != nil {
			return fmt.Errorf("scenario schema: %w", err)
		}
		if !schemaReady {
			return fmt.Errorf("scenario schema is missing: apply migrations/002_scenarios.sql")
		}
	}
	redisClient := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "redis:6379")})
	defer redisClient.Close()
	brokers := strings.Split(env("KAFKA_BROKERS", "kafka:9092"), ",")
	topic := env("KAFKA_TOPIC", "ai-buddy-requests")
	writer := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: topic, Balancer: &kafka.Hash{}, RequiredAcks: kafka.RequireAll}
	defer writer.Close()
	system := &distributed.System{DB: pool, Redis: redisClient, Writer: writer, Topic: topic, MaxTries: envInt("JOB_MAX_ATTEMPTS", 3)}
	service := rag.New(cfg.RAG)
	readyCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
	defer cancel()
	if err := service.WaitReady(readyCtx); err != nil {
		return fmt.Errorf("dependencies: %w", err)
	}
	if os.Getenv("APP_ROLE") == "worker" {
		reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, GroupID: env("KAFKA_GROUP", "ai-buddy-workers"), StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 10e6})
		defer reader.Close()
		system.Reader = reader
		answer := answerer{service: service, url: env("LLM_URL", "http://host.docker.internal:1234"), model: env("LLM_MODEL", "qwen3-8b"), mcpURL: env("MCP_URL", "http://api:8000/mcp"), mcpKey: cfg.APIKey, jiraMode: cfg.Jira.Mode, teamMCPURL: env("TEAM_MCP_URL", "http://team-access:8080/mcp"), rightsMCPURL: env("RIGHTS_MCP_URL", "http://access-rights:8080/mcp"), historyDB: pool}
		return system.Work(ctx, func(ctx context.Context, question string) (string, any, error) {
			text, sources, err := answer.Ask(ctx, question)
			if err != nil {
				log.Printf("agent answer failed: %v", err)
			}
			return text, sources, err
		}, time.Duration(envInt("JOB_TIMEOUT_SECONDS", 180))*time.Second)
	}
	if cfg.IndexOnStartup {
		if _, err := service.IndexIfEmpty(readyCtx); err != nil {
			return fmt.Errorf("initial indexing: %w", err)
		}
	}
	cfg.Distributed = system
	if os.Getenv("TEAM_ACCESS_URL") != "" {
		go consumeTeamEvents(ctx, pool, brokers)
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				workCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if err := system.RequeueExpired(workCtx); err != nil {
					log.Printf("job recovery: %v", err)
				}
				if err := system.PublishPending(workCtx); err != nil {
					log.Printf("job publisher: %v", err)
				}
				cancel()
			}
		}
	}()
	log.Printf("distributed API listens on :8000")
	handler := demoSession(os.Getenv("SESSION_SECRET"), logging(newHandler(service, cfg)))
	return http.ListenAndServe(":8000", handler)
}
