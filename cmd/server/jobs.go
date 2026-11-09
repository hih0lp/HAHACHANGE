package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/hackchange/avito-rag/internal/rag"
)

var errQueueFull = errors.New("job queue is full")

type jobSnapshot struct {
	ID      string          `json:"id"`
	Query   string          `json:"query"`
	Status  string          `json:"status"`
	Answer  string          `json:"answer,omitempty"`
	Sources []rag.SearchHit `json:"sources,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type askFunc func(context.Context, string) (string, []rag.SearchHit, error)

type jobEntry struct {
	data        jobSnapshot
	subscribers map[chan jobSnapshot]struct{}
	finishedAt  time.Time
}

type jobManager struct {
	mu      sync.Mutex
	jobs    map[string]*jobEntry
	queue   chan *jobEntry
	ask     askFunc
	timeout time.Duration
	ctx     context.Context
	cancel  context.CancelFunc
}

func newJobManager(workers, queueSize int, timeout time.Duration, ask askFunc) *jobManager {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &jobManager{jobs: make(map[string]*jobEntry), queue: make(chan *jobEntry, queueSize), ask: ask, timeout: timeout, ctx: ctx, cancel: cancel}
	for range workers {
		go m.worker()
	}
	return m
}

func (m *jobManager) close() { m.cancel() }

func (m *jobManager) submit(query string) (jobSnapshot, error) {
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return jobSnapshot{}, err
	}
	entry := &jobEntry{data: jobSnapshot{ID: hex.EncodeToString(idBytes), Query: query, Status: "queued"}, subscribers: make(map[chan jobSnapshot]struct{})}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pruneLocked(time.Now())
	select {
	case m.queue <- entry:
		m.jobs[entry.data.ID] = entry
		return entry.data, nil
	default:
		return jobSnapshot{}, errQueueFull
	}
}

func (m *jobManager) get(id string) (jobSnapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.jobs[id]
	if !ok {
		return jobSnapshot{}, false
	}
	return entry.data, true
}

func (m *jobManager) subscribe(id string) (<-chan jobSnapshot, func(), bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.jobs[id]
	if !ok {
		return nil, nil, false
	}
	ch := make(chan jobSnapshot, 1)
	ch <- entry.data
	entry.subscribers[ch] = struct{}{}
	return ch, func() { m.mu.Lock(); delete(entry.subscribers, ch); m.mu.Unlock() }, true
}

func (m *jobManager) worker() {
	for {
		select {
		case <-m.ctx.Done():
			return
		case entry := <-m.queue:
			m.update(entry, "running", "", nil, "")
			ctx, cancel := context.WithTimeout(m.ctx, m.timeout)
			answer, sources, err := m.ask(ctx, entry.data.Query)
			cancel()
			if err != nil {
				m.update(entry, "failed", "", nil, err.Error())
			} else {
				m.update(entry, "completed", answer, sources, "")
			}
		}
	}
}

func (m *jobManager) update(entry *jobEntry, status, answer string, sources []rag.SearchHit, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry.data.Status, entry.data.Answer, entry.data.Sources, entry.data.Error = status, answer, sources, message
	if status == "completed" || status == "failed" {
		entry.finishedAt = time.Now()
	}
	for ch := range entry.subscribers {
		select {
		case ch <- entry.data:
		default:
			select {
			case <-ch:
			default:
			}
			ch <- entry.data
		}
	}
}

func (m *jobManager) pruneLocked(now time.Time) {
	for id, entry := range m.jobs {
		if !entry.finishedAt.IsZero() && now.Sub(entry.finishedAt) > 30*time.Minute {
			delete(m.jobs, id)
		}
	}
}
