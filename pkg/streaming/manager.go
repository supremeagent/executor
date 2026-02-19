package streaming

import (
	"log"
	"sync"
)

// LogEntry stores a log entry
type LogEntry struct {
	Type    string      `json:"type"`
	Content interface{} `json:"content"`
}

// Manager manages SSE streams for executor sessions
type Manager struct {
	sessions    map[string][]LogEntry
	subscribers map[string][]chan LogEntry
	mu          sync.RWMutex
}

// NewManager creates a new SSE manager
func NewManager() *Manager {
	return &Manager{
		sessions:    make(map[string][]LogEntry),
		subscribers: make(map[string][]chan LogEntry),
	}
}

// StoreLogs stores logs for a session
func (m *Manager) StoreLogs(sessionID string, logs []LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = logs
}

// AppendLog appends a log entry to a session and notifies subscribers
func (m *Manager) AppendLog(sessionID string, entry LogEntry) {
	m.mu.Lock()
	m.sessions[sessionID] = append(m.sessions[sessionID], entry)
	
	// Copy subscribers to avoid holding lock during send
	subs := make([]chan LogEntry, len(m.subscribers[sessionID]))
	copy(subs, m.subscribers[sessionID])
	m.mu.Unlock()

	log.Printf("[DEBUG] AppendLog: session=%s, type=%s, subscribers=%d", sessionID, entry.Type, len(subs))

	// Notify all subscribers
	for i, ch := range subs {
		select {
		case ch <- entry:
			log.Printf("[DEBUG] AppendLog: sent to subscriber %d", i)
		default:
			log.Printf("[DEBUG] AppendLog: subscriber %d channel full, skipped", i)
		}
	}
}

// Subscribe subscribes to new logs for a session
func (m *Manager) Subscribe(sessionID string) (<-chan LogEntry, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ch := make(chan LogEntry, 100)
	m.subscribers[sessionID] = append(m.subscribers[sessionID], ch)
	log.Printf("[DEBUG] Subscribe: session=%s, total_subscribers=%d", sessionID, len(m.subscribers[sessionID]))

	unsubscribe := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		subs := m.subscribers[sessionID]
		for i, sub := range subs {
			if sub == ch {
				m.subscribers[sessionID] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}

	return ch, unsubscribe
}

// GetSession returns stored logs for a session
func (m *Manager) GetSession(sessionID string) ([]LogEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	logs, ok := m.sessions[sessionID]
	return logs, ok
}

// UnregisterSession unregisters a session and its subscribers
func (m *Manager) UnregisterSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.sessions, sessionID)
	
	for _, ch := range m.subscribers[sessionID] {
		close(ch)
	}
	delete(m.subscribers, sessionID)
}
