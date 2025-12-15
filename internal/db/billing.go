package db

import (
	"context"
	"fmt"
	"time"
)

// Subscription represents a user's subscription.
type Subscription struct {
	ID                   string     `json:"id"`
	UserID               string     `json:"user_id"`
	Plan                 string     `json:"plan"` // "free", "pro"
	StripeCustomerID     *string    `json:"stripe_customer_id,omitempty"`
	StripeSubscriptionID *string    `json:"stripe_subscription_id,omitempty"`
	CurrentPeriodStart   *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd     *time.Time `json:"current_period_end,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// Usage represents a usage record.
type Usage struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id"`
	SessionID    *string   `json:"session_id,omitempty"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CreatedAt    time.Time `json:"created_at"`
}

// Free tier limits
const (
	FreeTierMonthlyTokens = 1_000_000 // 1M tokens per month for free tier
)

// GetOrCreateSubscription gets or creates a subscription for a user.
func (c *Client) GetOrCreateSubscription(userID string) (*Subscription, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sub Subscription
	err := c.pool.QueryRow(ctx,
		`SELECT id, user_id, plan, stripe_customer_id, stripe_subscription_id,
		        current_period_start, current_period_end, created_at, updated_at
		 FROM subscriptions WHERE user_id = $1`,
		userID,
	).Scan(&sub.ID, &sub.UserID, &sub.Plan, &sub.StripeCustomerID, &sub.StripeSubscriptionID,
		&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CreatedAt, &sub.UpdatedAt)

	if err == nil {
		return &sub, nil
	}

	// Create new subscription with free plan
	err = c.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (user_id, plan) VALUES ($1, 'free')
		 RETURNING id, user_id, plan, stripe_customer_id, stripe_subscription_id,
		           current_period_start, current_period_end, created_at, updated_at`,
		userID,
	).Scan(&sub.ID, &sub.UserID, &sub.Plan, &sub.StripeCustomerID, &sub.StripeSubscriptionID,
		&sub.CurrentPeriodStart, &sub.CurrentPeriodEnd, &sub.CreatedAt, &sub.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	return &sub, nil
}

// UpdateSubscription updates a subscription's Stripe details.
func (c *Client) UpdateSubscription(userID, plan, stripeCustomerID, stripeSubscriptionID string, periodStart, periodEnd time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.pool.Exec(ctx,
		`UPDATE subscriptions
		 SET plan = $1, stripe_customer_id = $2, stripe_subscription_id = $3,
		     current_period_start = $4, current_period_end = $5, updated_at = NOW()
		 WHERE user_id = $6`,
		plan, stripeCustomerID, stripeSubscriptionID, periodStart, periodEnd, userID,
	)
	if err != nil {
		return fmt.Errorf("failed to update subscription: %w", err)
	}

	return nil
}

// RecordUsage records token usage for a session.
func (c *Client) RecordUsage(userID, sessionID string, inputTokens, outputTokens int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var sessID *string
	if sessionID != "" {
		sessID = &sessionID
	}

	_, err := c.pool.Exec(ctx,
		`INSERT INTO usage (user_id, session_id, input_tokens, output_tokens)
		 VALUES ($1, $2, $3, $4)`,
		userID, sessID, inputTokens, outputTokens,
	)
	if err != nil {
		return fmt.Errorf("failed to record usage: %w", err)
	}

	return nil
}

// GetMonthlyUsage gets total token usage for a user in the current month.
func (c *Client) GetMonthlyUsage(userID string) (inputTokens, outputTokens int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		 FROM usage
		 WHERE user_id = $1
		   AND created_at >= date_trunc('month', NOW())`,
		userID,
	).Scan(&inputTokens, &outputTokens)

	if err != nil {
		return 0, 0, fmt.Errorf("failed to get monthly usage: %w", err)
	}

	return inputTokens, outputTokens, nil
}

// GetPeriodUsage gets total token usage for a user within a subscription period.
func (c *Client) GetPeriodUsage(userID string, periodStart time.Time) (inputTokens, outputTokens int64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = c.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0)
		 FROM usage
		 WHERE user_id = $1
		   AND created_at >= $2`,
		userID, periodStart,
	).Scan(&inputTokens, &outputTokens)

	if err != nil {
		return 0, 0, fmt.Errorf("failed to get period usage: %w", err)
	}

	return inputTokens, outputTokens, nil
}

// CheckQuota checks if a user is within their quota.
// Returns (allowed, remainingTokens, error)
func (c *Client) CheckQuota(userID string) (bool, int64, error) {
	sub, err := c.GetOrCreateSubscription(userID)
	if err != nil {
		return false, 0, err
	}

	// Pro users have unlimited quota
	if sub.Plan == "pro" {
		return true, -1, nil // -1 means unlimited
	}

	// Free tier: check monthly usage
	input, output, err := c.GetMonthlyUsage(userID)
	if err != nil {
		return false, 0, err
	}

	totalUsed := input + output
	remaining := FreeTierMonthlyTokens - totalUsed

	if remaining <= 0 {
		return false, 0, nil
	}

	return true, remaining, nil
}

// GetSessionByConnectToken gets a session by its connect token.
func (c *Client) GetSessionByConnectToken(token string) (*Session, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var session Session
	err := c.pool.QueryRow(ctx,
		`SELECT id, user_id, machine_id, label, connect_token, connect_url, region, status, created_at, ended_at
		 FROM sessions WHERE connect_token = $1`,
		token,
	).Scan(&session.ID, &session.UserID, &session.MachineID, &session.Label, &session.ConnectToken,
		&session.ConnectURL, &session.Region, &session.Status, &session.CreatedAt, &session.EndedAt)

	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	return &session, nil
}
