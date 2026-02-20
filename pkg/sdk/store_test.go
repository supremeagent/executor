package sdk

import (
	"context"
	"testing"
	"time"
)

func TestMemoryEventStoreCleanupAfterDoneTTL(t *testing.T) {
	store := NewMemoryEventStoreWithOptions(MemoryEventStoreOptions{
		ExpireAfterDone: 80 * time.Millisecond,
		CleanupInterval: 20 * time.Millisecond,
	})
	defer store.Close()

	sessionID := "session-done-expire"
	_, _ = store.Append(context.Background(), Event{SessionID: sessionID, Type: "stdout", Content: "hello"})
	_, _ = store.Append(context.Background(), Event{SessionID: sessionID, Type: "done", Content: "done"})

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		events, err := store.List(context.Background(), sessionID, ListOptions{})
		if err != nil {
			t.Fatalf("list failed: %v", err)
		}
		if len(events) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected session to be cleaned up, still has %d events", len(events))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestMemoryEventStoreDoesNotCleanupRunningSession(t *testing.T) {
	store := NewMemoryEventStoreWithOptions(MemoryEventStoreOptions{
		ExpireAfterDone: 60 * time.Millisecond,
		CleanupInterval: 20 * time.Millisecond,
	})
	defer store.Close()

	sessionID := "session-running-keep"
	_, _ = store.Append(context.Background(), Event{SessionID: sessionID, Type: "stdout", Content: "still running"})

	time.Sleep(200 * time.Millisecond)
	events, err := store.List(context.Background(), sessionID, ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected running session events to be kept")
	}
}

func TestMemoryEventStoreWithExpirationHelper(t *testing.T) {
	store := NewMemoryEventStoreWithExpiration(50 * time.Millisecond)
	defer store.Close()

	if store.expireAfterDone <= 0 {
		t.Fatalf("expected expireAfterDone to be set")
	}
}
