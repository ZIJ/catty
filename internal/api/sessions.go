package api

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
type SessionStore struct {
	path     string
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionStore creates a new session store.
// Sessions are stored in ~/.catty/sessions.json.
func NewSessionStore() (*SessionStore, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home directory: %w", err)
	}

	cattyDir := filepath.Join(homeDir, ".catty")
	if err := os.MkdirAll(cattyDir, 0700); err != nil {
		return nil, fmt.Errorf("create .catty directory: %w", err)
	}

	store := &SessionStore{
		path:     filepath.Join(cattyDir, "sessions.json"),
		sessions: make(map[string]*Session),
	}

	if err := store.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("load sessions: %w", err)
	}

	return store, nil
}

// load reads sessions from disk.
func (s *SessionStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}

	var sessions []*Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("unmarshal sessions: %w", err)
	}

	s.sessions = make(map[string]*Session)
	for _, sess := range sessions {
		s.sessions[sess.SessionID] = sess
	}

	return nil
}

// save writes sessions to disk.
func (s *SessionStore) save() error {
	sessions := make([]*Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sessions: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write sessions file: %w", err)
	}

	return nil
}

// Save stores a new session.
func (s *SessionStore) Save(session *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.SessionID] = session
	return s.save()
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
	return s.save()
}
