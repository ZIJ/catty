package api

import (
	"sync"
	"time"
)

// Session represents a TUI session record.
type Session struct {
	SessionID    string    `json:"session_id"`
	MachineID    string    `json:"machine_id"`
	ConnectToken string    `json:"connect_token"`
	ConnectURL   string    `json:"connect_url"`
	Region       string    `json:"region"`
	CreatedAt    time.Time `json:"created_at"`
}

// SessionStore manages session persistence.
// Uses in-memory storage - sessions are ephemeral and can be reconstructed
// from Fly Machine metadata if needed.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionStore creates a new session store.
func NewSessionStore() (*SessionStore, error) {
	return &SessionStore{
		sessions: make(map[string]*Session),
	}, nil
}

// Save stores a new session.
func (s *SessionStore) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.SessionID] = session
	return nil
}

// Get retrieves a session by ID.
func (s *SessionStore) Get(sessionID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[sessionID]
	return session, ok
}

// List returns all sessions.
func (s *SessionStore) List() []*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	return sessions
}

// Delete removes a session by ID.
func (s *SessionStore) Delete(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sessionID)
	return nil
}
