package main

import (
	"context"
	"fmt"
	"log"
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
	return runDistributedContext(ctx, cfg)
}

func runDistributedContext(ctx context.Context, cfg config) error {
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
	if err := pool.QueryRow(ctx, `SELECT to_regclass('ai_jobs') IS NOT NULL`).Scan(&schemaReady); err != nil {
		return fmt.Errorf("job schema: %w", err)
	}
	if !schemaReady {
		return fmt.Errorf("job schema is missing: apply migrations/001_distributed_jobs.sql")
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
		reader := kafka.NewReader(kafka.ReaderConfig{Brokers: brokers, Topic: topic, GroupID: env("KAFKA_GROUP", "ai-buddy-workers"), MinBytes: 1, MaxBytes: 10e6})
		defer reader.Close()
		system.Reader = reader
		answer := answerer{service: service, url: env("LLM_URL", "http://host.docker.internal:1234"), model: env("LLM_MODEL", "qwen3-8b"), mcpURL: env("MCP_URL", "http://api:8000/mcp"), mcpKey: cfg.APIKey, jiraMode: cfg.Jira.Mode}
		return system.Work(ctx, func(ctx context.Context, question string) (string, any, error) {
			text, sources, err := answer.Ask(ctx, question)
			return text, sources, err
		}, time.Duration(envInt("JOB_TIMEOUT_SECONDS", 180))*time.Second)
	}
	if cfg.IndexOnStartup {
		if _, err := service.IndexIfEmpty(readyCtx); err != nil {
			return fmt.Errorf("initial indexing: %w", err)
		}
	}
	cfg.Distributed = system
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
	return listenAndServe(":8000", handler)
}
