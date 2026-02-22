package store

import (
	"context"
	"sync"
	"time"

	"github.com/supremeagent/executor/pkg/executor"
)

// ListOptions controls event list query behavior.
type ListOptions struct {
	AfterSeq uint64
	UntilSeq uint64
	Limit    int
}

// EventStore persists execution events.
type EventStore interface {
	Append(ctx context.Context, evt executor.Event) (executor.Event, error)
	List(ctx context.Context, sessionID string, opts ListOptions) ([]executor.Event, error)
	LatestSeq(ctx context.Context, sessionID string) (uint64, error)
}

// MemoryEventStore is the default in-memory EventStore implementation.
type MemoryEventStore struct {
	mu              sync.RWMutex
	events          map[string][]executor.Event
	nextSeq         map[string]uint64
	sessionDoneAt   map[string]time.Time
	expireAfterDone time.Duration
	cleanupInterval time.Duration
	stopCleanup     chan struct{}
	stopOnce        sync.Once
}

// MemoryEventStoreOptions controls in-memory store lifecycle behavior.
type MemoryEventStoreOptions struct {
	// ExpireAfterDone removes a session after it has been in done state for this long.
	// Set to 0 to disable automatic cleanup.
	ExpireAfterDone time.Duration
	// CleanupInterval controls how often expired sessions are scanned and removed.
	// If <= 0 and ExpireAfterDone > 0, a sensible default is applied.
	CleanupInterval time.Duration
}

// NewMemoryEventStore creates an in-memory event store.
func NewMemoryEventStore() *MemoryEventStore {
	return NewMemoryEventStoreWithOptions(MemoryEventStoreOptions{})
}

// NewMemoryEventStoreWithExpiration creates an in-memory store with done-session TTL.
func NewMemoryEventStoreWithExpiration(expireAfterDone time.Duration) *MemoryEventStore {
	return NewMemoryEventStoreWithOptions(MemoryEventStoreOptions{
		ExpireAfterDone: expireAfterDone,
	})
}

// NewMemoryEventStoreWithOptions creates an in-memory store with custom options.
func NewMemoryEventStoreWithOptions(opts MemoryEventStoreOptions) *MemoryEventStore {
	store := &MemoryEventStore{
		events:          make(map[string][]executor.Event),
		nextSeq:         make(map[string]uint64),
		sessionDoneAt:   make(map[string]time.Time),
		expireAfterDone: opts.ExpireAfterDone,
		cleanupInterval: opts.CleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	if store.expireAfterDone > 0 {
		if store.cleanupInterval <= 0 {
			store.cleanupInterval = minDuration(30*time.Second, store.expireAfterDone)
		}
		go store.cleanupLoop()
	}

	return store
}

func (s *MemoryEventStore) Append(ctx context.Context, evt executor.Event) (executor.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	evt.Seq = s.nextSeq[evt.SessionID] + 1
	s.nextSeq[evt.SessionID] = evt.Seq
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now()
	}

	s.events[evt.SessionID] = append(s.events[evt.SessionID], evt)
	if evt.Type == "done" {
		s.sessionDoneAt[evt.SessionID] = evt.Timestamp
	}
	return evt, nil
}

func (s *MemoryEventStore) List(ctx context.Context, sessionID string, opts ListOptions) ([]executor.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	src := s.events[sessionID]
	if len(src) == 0 {
		return nil, nil
	}

	out := make([]executor.Event, 0, len(src))
	for _, evt := range src {
		if opts.AfterSeq > 0 && evt.Seq <= opts.AfterSeq {
			continue
		}
		if opts.UntilSeq > 0 && evt.Seq > opts.UntilSeq {
			continue
		}
		out = append(out, evt)
		if opts.Limit > 0 && len(out) >= opts.Limit {
			break
		}
	}

	return out, nil
}

func (s *MemoryEventStore) LatestSeq(ctx context.Context, sessionID string) (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seq, ok := s.nextSeq[sessionID]
	if !ok {
		return 0, nil
	}
	return seq, nil
}

// Close stops the cleanup goroutine for stores created with expiration options.
func (s *MemoryEventStore) Close() {
	s.stopOnce.Do(func() {
		close(s.stopCleanup)
	})
}

func (s *MemoryEventStore) cleanupLoop() {
	ticker := time.NewTicker(s.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.cleanupExpiredSessions(time.Now())
		case <-s.stopCleanup:
			return
		}
	}
}

func (s *MemoryEventStore) cleanupExpiredSessions(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sessionID, doneAt := range s.sessionDoneAt {
		if now.Sub(doneAt) < s.expireAfterDone {
			continue
		}

		delete(s.events, sessionID)
		delete(s.nextSeq, sessionID)
		delete(s.sessionDoneAt, sessionID)
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
