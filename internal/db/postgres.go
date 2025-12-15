package db

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Client is a PostgreSQL database client.
type Client struct {
	pool *pgxpool.Pool
}

// NewClient creates a new database client from environment variables.
// Expects DATABASE_URL in standard PostgreSQL format:
// postgresql://user:password@host:port/database
func NewClient() (*Client, error) {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		return nil, fmt.Errorf("DATABASE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Client{pool: pool}, nil
}

// Close closes the database connection pool.
func (c *Client) Close() {
	c.pool.Close()
}

// User represents a user in the database.
type User struct {
	ID        string    `json:"id"`
	WorkosID  string    `json:"workos_id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Session represents a session in the database.
type Session struct {
	ID           string     `json:"id"`
	UserID       string     `json:"user_id"`
	MachineID    string     `json:"machine_id"`
	Label        string     `json:"label"`
	ConnectToken string     `json:"connect_token"`
	ConnectURL   string     `json:"connect_url"`
	Region       string     `json:"region"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	EndedAt      *time.Time `json:"ended_at"`
}

// GetOrCreateUser gets a user by WorkOS ID, or creates one if not found.
func (c *Client) GetOrCreateUser(workosID, email string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try to get existing user
	var user User
	err := c.pool.QueryRow(ctx,
		`SELECT id, workos_id, email, created_at FROM users WHERE workos_id = $1`,
		workosID,
	).Scan(&user.ID, &user.WorkosID, &user.Email, &user.CreatedAt)

	if err == nil {
		return &user, nil
	}

	// Create new user
	err = c.pool.QueryRow(ctx,
		`INSERT INTO users (workos_id, email) VALUES ($1, $2)
		 RETURNING id, workos_id, email, created_at`,
		workosID, email,
	).Scan(&user.ID, &user.WorkosID, &user.Email, &user.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &user, nil
}

// GetUserByWorkosID gets a user by their WorkOS ID.
func (c *Client) GetUserByWorkosID(workosID string) (*User, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var user User
	err := c.pool.QueryRow(ctx,
		`SELECT id, workos_id, email, created_at FROM users WHERE workos_id = $1`,
		workosID,
	).Scan(&user.ID, &user.WorkosID, &user.Email, &user.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &user, nil
}

// CreateSession creates a new session.
func (c *Client) CreateSession(session *Session) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := c.pool.QueryRow(ctx,
		`INSERT INTO sessions (user_id, machine_id, label, connect_token, connect_url, region, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, user_id, machine_id, label, connect_token, connect_url, region, status, created_at, ended_at`,
		session.UserID, session.MachineID, session.Label, session.ConnectToken, session.ConnectURL, session.Region, session.Status,
	).Scan(&session.ID, &session.UserID, &session.MachineID, &session.Label, &session.ConnectToken,
		&session.ConnectURL, &session.Region, &session.Status, &session.CreatedAt, &session.EndedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session, nil
}

// GetSessionByLabel gets a session by its label for a specific user.
func (c *Client) GetSessionByLabel(userID, label string) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var session Session
	err := c.pool.QueryRow(ctx,
		`SELECT id, user_id, machine_id, label, connect_token, connect_url, region, status, created_at, ended_at
		 FROM sessions WHERE user_id = $1 AND label = $2`,
		userID, label,
	).Scan(&session.ID, &session.UserID, &session.MachineID, &session.Label, &session.ConnectToken,
		&session.ConnectURL, &session.Region, &session.Status, &session.CreatedAt, &session.EndedAt)

	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	return &session, nil
}

// GetSessionByID gets a session by its ID.
func (c *Client) GetSessionByID(id string) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var session Session
	err := c.pool.QueryRow(ctx,
		`SELECT id, user_id, machine_id, label, connect_token, connect_url, region, status, created_at, ended_at
		 FROM sessions WHERE id = $1`,
		id,
	).Scan(&session.ID, &session.UserID, &session.MachineID, &session.Label, &session.ConnectToken,
		&session.ConnectURL, &session.Region, &session.Status, &session.CreatedAt, &session.EndedAt)

	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	return &session, nil
}

// ListUserSessions lists all sessions for a user.
func (c *Client) ListUserSessions(userID string) ([]Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := c.pool.Query(ctx,
		`SELECT id, user_id, machine_id, label, connect_token, connect_url, region, status, created_at, ended_at
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.MachineID, &s.Label, &s.ConnectToken,
			&s.ConnectURL, &s.Region, &s.Status, &s.CreatedAt, &s.EndedAt); err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, s)
	}

	return sessions, nil
}

// UpdateSessionStatus updates a session's status.
func (c *Client) UpdateSessionStatus(id, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var err error
	if status == "stopped" {
		_, err = c.pool.Exec(ctx,
			`UPDATE sessions SET status = $1, ended_at = NOW() WHERE id = $2`,
			status, id,
		)
	} else {
		_, err = c.pool.Exec(ctx,
			`UPDATE sessions SET status = $1 WHERE id = $2`,
			status, id,
		)
	}

	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// DeleteSession deletes a session by ID.
func (c *Client) DeleteSession(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}
