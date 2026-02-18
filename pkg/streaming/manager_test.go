package streaming

import (
	"testing"
	"time"
)

func TestManager(t *testing.T) {
	m := NewManager()
	sessionID := "test-session"

	// Test AppendLog and GetSession
	log1 := LogEntry{Type: "stdout", Content: "hello"}
	m.AppendLog(sessionID, log1)

	logs, ok := m.GetSession(sessionID)
	if !ok {
		t.Fatal("session should exist")
	}
	if len(logs) != 1 || logs[0].Content != "hello" {
		t.Errorf("unexpected logs: %v", logs)
	}

	// Test Subscribe
	ch, unsubscribe := m.Subscribe(sessionID)
	defer unsubscribe()

	log2 := LogEntry{Type: "stdout", Content: "world"}
	go m.AppendLog(sessionID, log2)

	select {
	case log := <-ch:
		if log.Content != "world" {
			t.Errorf("unexpected log from subscriber: %v", log)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for subscriber notification")
	}

	// Test UnregisterSession
	m.UnregisterSession(sessionID)
	_, ok = m.GetSession(sessionID)
	if ok {
		t.Error("session should be removed")
	}

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("subscriber channel should be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for channel close")
	}
}

func TestManager_StoreLogs(t *testing.T) {
	m := NewManager()
	sessionID := "test-session-store"
	logs := []LogEntry{
		{Type: "stdout", Content: "line 1"},
		{Type: "stdout", Content: "line 2"},
	}

	m.StoreLogs(sessionID, logs)
	retrieved, ok := m.GetSession(sessionID)
	if !ok {
		t.Fatal("session should exist")
	}
	if len(retrieved) != 2 {
		t.Errorf("expected 2 logs, got %d", len(retrieved))
	}
}
