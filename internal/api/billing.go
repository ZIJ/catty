package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/izalutski/catty/internal/db"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/checkout/session"
	"github.com/stripe/stripe-go/v76/customer"
	"github.com/stripe/stripe-go/v76/webhook"
)

// BillingHandlers handles billing-related requests.
type BillingHandlers struct {
	db              *db.Client
	stripeKey       string
	webhookSecret   string
	priceID         string
	successURL      string
	cancelURL       string
}

// NewBillingHandlers creates new billing handlers.
func NewBillingHandlers(dbClient *db.Client) (*BillingHandlers, error) {
	stripeKey := os.Getenv("STRIPE_SECRET_KEY")
	if stripeKey == "" {
		return nil, fmt.Errorf("STRIPE_SECRET_KEY is required")
	}

	webhookSecret := os.Getenv("STRIPE_WEBHOOK_SECRET")
	if webhookSecret == "" {
		return nil, fmt.Errorf("STRIPE_WEBHOOK_SECRET is required")
	}

	priceID := os.Getenv("STRIPE_PRICE_ID")
	if priceID == "" {
		return nil, fmt.Errorf("STRIPE_PRICE_ID is required")
	}

	// Get API host for redirect URLs
	apiHost := os.Getenv("CATTY_API_HOST")
	if apiHost == "" {
		apiHost = "api.catty.dev"
	}

	// Initialize Stripe
	stripe.Key = stripeKey

	return &BillingHandlers{
		db:            dbClient,
		stripeKey:     stripeKey,
		webhookSecret: webhookSecret,
		priceID:       priceID,
		successURL:    fmt.Sprintf("https://%s/billing/success", apiHost),
		cancelURL:     fmt.Sprintf("https://%s/billing/cancel", apiHost),
	}, nil
}

// CheckoutResponse is the response for creating a checkout session.
type CheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
}

// CreateCheckoutSession creates a Stripe Checkout session for upgrading to pro.
// Supports both POST (returns JSON) and GET (redirects to Stripe).
func (h *BillingHandlers) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	// Get authenticated user from context
	authUser := UserFromContext(r.Context())
	if authUser == nil {
		writeError(w, http.StatusUnauthorized, "user not found in context")
		return
	}

	// Get user from database
	dbUser, err := h.db.GetUserByWorkosID(authUser.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Get or create Stripe customer
	stripeCustomerID, err := h.getOrCreateStripeCustomer(dbUser)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create Stripe customer: "+err.Error())
		return
	}

	// Create Checkout session
	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(stripeCustomerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(h.priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL: stripe.String(h.successURL),
		CancelURL:  stripe.String(h.cancelURL),
		// Store user ID in metadata for webhook
		SubscriptionData: &stripe.CheckoutSessionSubscriptionDataParams{
			Metadata: map[string]string{
				"user_id": dbUser.ID,
			},
		},
	}

	sess, err := session.New(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create checkout session: "+err.Error())
		return
	}

	// For GET requests, redirect directly to Stripe
	if r.Method == http.MethodGet {
		http.Redirect(w, r, sess.URL, http.StatusFound)
		return
	}

	// For POST requests, return JSON
	writeJSON(w, http.StatusOK, &CheckoutResponse{
		CheckoutURL: sess.URL,
	})
}

// getOrCreateStripeCustomer gets or creates a Stripe customer for a user.
func (h *BillingHandlers) getOrCreateStripeCustomer(user *db.User) (string, error) {
	// Check if user already has a Stripe customer ID
	sub, err := h.db.GetOrCreateSubscription(user.ID)
	if err != nil {
		fmt.Printf("checkout: failed to get/create subscription: %v\n", err)
		return "", err
	}

	if sub.StripeCustomerID != nil && *sub.StripeCustomerID != "" {
		fmt.Printf("checkout: using existing stripe customer: %s\n", *sub.StripeCustomerID)
		return *sub.StripeCustomerID, nil
	}

	// Create new Stripe customer
	fmt.Printf("checkout: creating new stripe customer for user %s\n", user.ID)
	params := &stripe.CustomerParams{
		Email: stripe.String(user.Email),
		Metadata: map[string]string{
			"user_id":   user.ID,
			"workos_id": user.WorkosID,
		},
	}

	cust, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}
	fmt.Printf("checkout: created stripe customer %s\n", cust.ID)

	// Save Stripe customer ID
	if err := h.db.SetStripeCustomerID(user.ID, cust.ID); err != nil {
		fmt.Printf("checkout: failed to save stripe customer id: %v\n", err)
		return "", fmt.Errorf("save stripe customer id: %w", err)
	}
	fmt.Printf("checkout: saved stripe customer id to db\n")

	return cust.ID, nil
}

// HandleStripeWebhook handles Stripe webhook events.
func (h *BillingHandlers) HandleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	const MaxBodyBytes = int64(65536)
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		fmt.Printf("webhook: error reading body: %v\n", err)
		writeError(w, http.StatusServiceUnavailable, "error reading request body")
		return
	}

	// Verify webhook signature
	sigHeader := r.Header.Get("Stripe-Signature")
	fmt.Printf("webhook: received event, sig header present: %v, payload len: %d\n", sigHeader != "", len(payload))

	event, err := webhook.ConstructEventWithOptions(payload, sigHeader, h.webhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		fmt.Printf("webhook: signature verification failed: %v\n", err)
		fmt.Printf("webhook: secret starts with: %.10s...\n", h.webhookSecret)
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	fmt.Printf("webhook: verified event type: %s\n", event.Type)

	// Handle the event
	switch event.Type {
	case "checkout.session.completed":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			writeError(w, http.StatusBadRequest, "error parsing webhook JSON")
			return
		}
		h.handleCheckoutCompleted(&sess)

	case "customer.subscription.created":
		// Also handle subscription created (sometimes fires instead of checkout.session.completed)
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			writeError(w, http.StatusBadRequest, "error parsing webhook JSON")
			return
		}
		h.handleSubscriptionCreated(&sub)

	case "customer.subscription.deleted":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			writeError(w, http.StatusBadRequest, "error parsing webhook JSON")
			return
		}
		h.handleSubscriptionDeleted(&sub)

	case "customer.subscription.updated":
		var sub stripe.Subscription
		if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
			writeError(w, http.StatusBadRequest, "error parsing webhook JSON")
			return
		}
		h.handleSubscriptionUpdated(&sub)
	}

	w.WriteHeader(http.StatusOK)
}

// handleCheckoutCompleted upgrades user to pro after successful checkout.
func (h *BillingHandlers) handleCheckoutCompleted(sess *stripe.CheckoutSession) {
	// Get user ID from subscription metadata
	if sess.Subscription == nil {
		return
	}

	// We need to fetch the subscription to get metadata
	subID := sess.Subscription.ID
	customerID := ""
	if sess.Customer != nil {
		customerID = sess.Customer.ID
	}

	// Find user by Stripe customer ID
	userID, err := h.db.GetUserByStripeCustomerID(customerID)
	if err != nil {
		// Log error but don't fail webhook
		fmt.Printf("warning: could not find user for customer %s: %v\n", customerID, err)
		return
	}

	// Update subscription to pro
	periodStart := time.Now()
	periodEnd := periodStart.AddDate(0, 1, 0) // +1 month

	if err := h.db.UpdateSubscription(userID, "pro", customerID, subID, periodStart, periodEnd); err != nil {
		fmt.Printf("warning: failed to update subscription for user %s: %v\n", userID, err)
		return
	}

	fmt.Printf("User %s upgraded to pro\n", userID)
}

// handleSubscriptionCreated upgrades user to pro when subscription is created.
func (h *BillingHandlers) handleSubscriptionCreated(sub *stripe.Subscription) {
	customerID := ""
	if sub.Customer != nil {
		customerID = sub.Customer.ID
	}

	fmt.Printf("webhook: subscription created for customer %s\n", customerID)

	userID, err := h.db.GetUserByStripeCustomerID(customerID)
	if err != nil {
		fmt.Printf("warning: could not find user for customer %s: %v\n", customerID, err)
		return
	}

	// Update subscription to pro
	periodStart := time.Unix(sub.CurrentPeriodStart, 0)
	periodEnd := time.Unix(sub.CurrentPeriodEnd, 0)

	if err := h.db.UpdateSubscription(userID, "pro", customerID, sub.ID, periodStart, periodEnd); err != nil {
		fmt.Printf("warning: failed to update subscription for user %s: %v\n", userID, err)
		return
	}

	fmt.Printf("User %s upgraded to pro via subscription created\n", userID)
}

// handleSubscriptionDeleted downgrades user to free when subscription is cancelled.
func (h *BillingHandlers) handleSubscriptionDeleted(sub *stripe.Subscription) {
	customerID := ""
	if sub.Customer != nil {
		customerID = sub.Customer.ID
	}

	userID, err := h.db.GetUserByStripeCustomerID(customerID)
	if err != nil {
		fmt.Printf("warning: could not find user for customer %s: %v\n", customerID, err)
		return
	}

	// Downgrade to free (keep Stripe IDs for potential re-subscription)
	if err := h.db.UpdateSubscriptionPlan(userID, "free"); err != nil {
		fmt.Printf("warning: failed to downgrade subscription for user %s: %v\n", userID, err)
		return
	}

	fmt.Printf("User %s downgraded to free\n", userID)
}

// handleSubscriptionUpdated updates period dates when subscription renews.
func (h *BillingHandlers) handleSubscriptionUpdated(sub *stripe.Subscription) {
	customerID := ""
	if sub.Customer != nil {
		customerID = sub.Customer.ID
	}

	userID, err := h.db.GetUserByStripeCustomerID(customerID)
	if err != nil {
		return
	}

	periodStart := time.Unix(sub.CurrentPeriodStart, 0)
	periodEnd := time.Unix(sub.CurrentPeriodEnd, 0)

	if err := h.db.UpdateSubscriptionPeriod(userID, periodStart, periodEnd); err != nil {
		fmt.Printf("warning: failed to update period for user %s: %v\n", userID, err)
	}
}

// BillingSuccess serves the success page after checkout.
func (h *BillingHandlers) BillingSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Payment Successful - Catty</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f5f5f5;
        }
        .container {
            text-align: center;
            padding: 2rem;
            background: white;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            max-width: 400px;
        }
        h1 { color: #22c55e; margin-bottom: 1rem; }
        code {
            background: #f1f5f9;
            padding: 0.25rem 0.5rem;
            border-radius: 4px;
            font-size: 1rem;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>✓ Payment Successful!</h1>
        <p>You're now subscribed to Catty Pro.</p>
        <p>Return to your terminal and run <code>catty new</code> to start a session.</p>
    </div>
</body>
</html>`))
}

// BillingCancel serves the cancel page when user cancels checkout.
func (h *BillingHandlers) BillingCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
    <title>Payment Cancelled - Catty</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            display: flex;
            justify-content: center;
            align-items: center;
            min-height: 100vh;
            margin: 0;
            background: #f5f5f5;
        }
        .container {
            text-align: center;
            padding: 2rem;
            background: white;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            max-width: 400px;
        }
        h1 { color: #64748b; margin-bottom: 1rem; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Payment Cancelled</h1>
        <p>No charges were made.</p>
        <p>Return to your terminal to continue with the free tier.</p>
    </div>
</body>
</html>`))
}
