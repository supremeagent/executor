package streaming

import (
	"sync"
)

// LogEntry stores a log entry
type LogEntry struct {
	Type    string
	Content interface{}
}

// Manager manages SSE streams for executor sessions
type Manager struct {
	sessions    map[string][]LogEntry
	mu          sync.RWMutex
}

// NewManager creates a new SSE manager
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string][]LogEntry),
	}
}

// StoreLogs stores logs for a session
func (m *Manager) StoreLogs(sessionID string, logs []LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = logs
}

// AppendLog appends a log entry to a session
func (m *Manager) AppendLog(sessionID string, log LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[sessionID] = append(m.sessions[sessionID], log)
}

// GetSession returns stored logs for a session
func (m *Manager) GetSession(sessionID string) ([]LogEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	logs, ok := m.sessions[sessionID]
	return logs, ok
}

// UnregisterSession unregisters a session
func (m *Manager) UnregisterSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}
