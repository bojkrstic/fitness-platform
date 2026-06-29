package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"

	"fitnes-platform/internal/model"
	"fitnes-platform/internal/repository"
)

const (
	stripeAPIBaseURL           = "https://api.stripe.com/v1"
	stripeWebhookTolerance     = 5 * time.Minute
	subscriptionStatusActive   = "active"
	subscriptionStatusTrialing = "trialing"
)

type stripeCheckoutSession struct {
	ID              string            `json:"id"`
	Customer        string            `json:"customer"`
	Subscription    string            `json:"subscription"`
	Status          string            `json:"status"`
	PaymentStatus   string            `json:"payment_status"`
	ClientReference string            `json:"client_reference_id"`
	Mode            string            `json:"mode"`
	Metadata        map[string]string `json:"metadata"`
}

type stripeSubscription struct {
	ID                 string `json:"id"`
	Customer           string `json:"customer"`
	Status             string `json:"status"`
	CancelAtPeriodEnd  bool   `json:"cancel_at_period_end"`
	CurrentPeriodStart int64  `json:"current_period_start"`
	CurrentPeriodEnd   int64  `json:"current_period_end"`
	Items              struct {
		Data []struct {
			Price struct {
				ID string `json:"id"`
			} `json:"price"`
		} `json:"data"`
	} `json:"items"`
}

type stripeEvent struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data stripeEventData `json:"data"`
}

type stripeEventData struct {
	Object json.RawMessage `json:"object"`
}

func (s *Service) BillingEnabled() bool {
	return s.billing.AppBaseURL != "" && s.billing.StripeSecretKey != "" && s.billing.StripeWebhookSecret != "" && s.billing.StripePriceID != ""
}

func (s *Service) BillingDisabledReason() string {
	missing := make([]string, 0, 4)
	if s.billing.AppBaseURL == "" {
		missing = append(missing, "APP_BASE_URL")
	}
	if s.billing.StripeSecretKey == "" {
		missing = append(missing, "STRIPE_SECRET_KEY")
	}
	if s.billing.StripeWebhookSecret == "" {
		missing = append(missing, "STRIPE_WEBHOOK_SECRET")
	}
	if s.billing.StripePriceID == "" {
		missing = append(missing, "STRIPE_PRICE_ID")
	}
	if len(missing) == 0 {
		return ""
	}
	return "Billing is not configured. Missing: " + strings.Join(missing, ", ")
}

func (s *Service) SubscriptionForUser(ctx context.Context, userID string) (*model.Subscription, error) {
	sub, err := s.store.FindSubscriptionByUserID(ctx, userID)
	if err != nil {
		if err == repository.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &sub, nil
}

func (s *Service) UserCanAccess(ctx context.Context, user *model.User) (bool, *model.Subscription, error) {
	if user == nil {
		return false, nil, nil
	}
	if user.Role == "admin" {
		return true, nil, nil
	}
	if !s.BillingEnabled() {
		return true, nil, nil
	}

	sub, err := s.SubscriptionForUser(ctx, user.ID)
	if err != nil {
		return false, nil, err
	}
	if sub == nil {
		return false, nil, nil
	}

	return subscriptionAllowsAccess(sub.Status), sub, nil
}

func subscriptionAllowsAccess(status string) bool {
	switch status {
	case subscriptionStatusActive, subscriptionStatusTrialing:
		return true
	default:
		return false
	}
}

func (s *Service) CreateCheckoutSession(ctx context.Context, user *model.User, returnTo string) (string, error) {
	if !s.BillingEnabled() {
		return "", ErrStorageDisabled
	}
	if user == nil {
		return "", ErrInvalidCredentials
	}

	customerID, err := s.ensureBillingCustomer(ctx, user)
	if err != nil {
		return "", err
	}

	successBaseURL, err := s.absoluteURL("/billing/complete")
	if err != nil {
		return "", err
	}
	successURL, err := buildCheckoutSuccessURL(successBaseURL, returnTo)
	if err != nil {
		return "", err
	}
	cancelURL, err := s.billingRedirectURL(returnTo, map[string]string{
		"billing": "canceled",
	})
	if err != nil {
		return "", err
	}

	form := neturl.Values{}
	form.Set("mode", "subscription")
	form.Set("customer", customerID)
	form.Set("line_items[0][price]", s.billing.StripePriceID)
	form.Set("line_items[0][quantity]", "1")
	form.Set("success_url", successURL)
	form.Set("cancel_url", cancelURL)
	form.Set("client_reference_id", user.ID)
	form.Set("allow_promotion_codes", "true")
	form.Set("metadata[user_id]", user.ID)
	form.Set("metadata[user_email]", user.Email)

	var resp struct {
		URL string `json:"url"`
	}
	if err := s.stripeRequest(ctx, http.MethodPost, "/checkout/sessions", form, &resp); err != nil {
		return "", err
	}

	return resp.URL, nil
}

func (s *Service) CreateBillingPortalSession(ctx context.Context, user *model.User, returnTo string) (string, error) {
	if !s.BillingEnabled() {
		return "", ErrStorageDisabled
	}
	if user == nil {
		return "", ErrInvalidCredentials
	}

	customer, err := s.store.FindBillingCustomerByUserID(ctx, user.ID)
	if err != nil {
		return "", err
	}

	returnURL, err := s.absoluteURL(returnTo)
	if err != nil {
		return "", err
	}

	form := neturl.Values{}
	form.Set("customer", customer.StripeCustomerID)
	form.Set("return_url", returnURL)

	var resp struct {
		URL string `json:"url"`
	}
	if err := s.stripeRequest(ctx, http.MethodPost, "/billing_portal/sessions", form, &resp); err != nil {
		return "", err
	}

	return resp.URL, nil
}

func (s *Service) FinalizeCheckoutSession(ctx context.Context, sessionID string) error {
	if !s.BillingEnabled() {
		return ErrStorageDisabled
	}

	var session stripeCheckoutSession
	if err := s.stripeRequest(ctx, http.MethodGet, "/checkout/sessions/"+sessionID, nil, &session); err != nil {
		return err
	}
	if session.Mode != "subscription" {
		return fmt.Errorf("unexpected checkout session mode: %s", session.Mode)
	}
	if session.Customer == "" || session.Subscription == "" {
		return fmt.Errorf("checkout session is missing customer or subscription")
	}

	userID := session.ClientReference
	if userID == "" && session.Metadata != nil {
		userID = session.Metadata["user_id"]
	}
	if userID == "" {
		userID, _ = s.store.FindUserIDByBillingCustomerID(ctx, session.Customer)
	}
	if userID == "" {
		return fmt.Errorf("cannot resolve user for checkout session %s", session.ID)
	}

	if err := s.syncSubscriptionByID(ctx, userID, session.Customer, session.Subscription); err != nil {
		return err
	}

	return nil
}

func (s *Service) HandleStripeWebhook(ctx context.Context, payload []byte, signature string) error {
	if !s.BillingEnabled() {
		return ErrStorageDisabled
	}

	event, err := verifyStripeSignature(payload, signature, s.billing.StripeWebhookSecret)
	if err != nil {
		return err
	}

	switch event.Type {
	case "checkout.session.completed":
		var session stripeCheckoutSession
		if err := json.Unmarshal(event.Data.Object, &session); err != nil {
			return fmt.Errorf("parse checkout session: %w", err)
		}
		if session.Customer == "" || session.Subscription == "" {
			return nil
		}

		userID := session.ClientReference
		if userID == "" && session.Metadata != nil {
			userID = session.Metadata["user_id"]
		}
		if userID == "" {
			var lookupErr error
			userID, lookupErr = s.store.FindUserIDByBillingCustomerID(ctx, session.Customer)
			if lookupErr != nil && lookupErr != repository.ErrNotFound {
				return lookupErr
			}
		}
		if userID == "" {
			return nil
		}

		if err := s.syncSubscriptionByID(ctx, userID, session.Customer, session.Subscription); err != nil {
			return err
		}
		_, err := s.store.MarkStripeWebhookEvent(ctx, event.ID, event.Type)
		return err

	case "customer.subscription.created", "customer.subscription.updated", "customer.subscription.deleted":
		var subscription stripeSubscription
		if err := json.Unmarshal(event.Data.Object, &subscription); err != nil {
			return fmt.Errorf("parse subscription: %w", err)
		}
		if subscription.ID == "" || subscription.Customer == "" {
			return nil
		}

		userID, err := s.store.FindUserIDByBillingCustomerID(ctx, subscription.Customer)
		if err != nil {
			if err == repository.ErrNotFound {
				return nil
			}
			return err
		}
		if err := s.syncSubscriptionFromStripe(ctx, userID, subscription); err != nil {
			return err
		}
		_, err = s.store.MarkStripeWebhookEvent(ctx, event.ID, event.Type)
		return err

	case "invoice.payment_succeeded", "invoice.payment_failed":
		var invoice struct {
			Subscription string `json:"subscription"`
			Customer     string `json:"customer"`
		}
		if err := json.Unmarshal(event.Data.Object, &invoice); err != nil {
			return fmt.Errorf("parse invoice: %w", err)
		}
		if invoice.Subscription == "" || invoice.Customer == "" {
			return nil
		}
		userID, err := s.store.FindUserIDByBillingCustomerID(ctx, invoice.Customer)
		if err != nil {
			if err == repository.ErrNotFound {
				return nil
			}
			return err
		}
		if err := s.syncSubscriptionByID(ctx, userID, invoice.Customer, invoice.Subscription); err != nil {
			return err
		}
		_, err = s.store.MarkStripeWebhookEvent(ctx, event.ID, event.Type)
		return err
	default:
		return nil
	}
}

func (s *Service) ensureBillingCustomer(ctx context.Context, user *model.User) (string, error) {
	customer, err := s.store.FindBillingCustomerByUserID(ctx, user.ID)
	if err == nil {
		return customer.StripeCustomerID, nil
	}
	if err != nil && err != repository.ErrNotFound {
		return "", err
	}

	form := neturl.Values{}
	form.Set("email", user.Email)
	form.Set("metadata[user_id]", user.ID)
	form.Set("metadata[user_email]", user.Email)

	var resp struct {
		ID string `json:"id"`
	}
	if err := s.stripeRequest(ctx, http.MethodPost, "/customers", form, &resp); err != nil {
		return "", err
	}

	if err := s.store.UpsertBillingCustomer(ctx, user.ID, resp.ID); err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (s *Service) syncSubscriptionByID(ctx context.Context, userID, customerID, subscriptionID string) error {
	var subscription stripeSubscription
	if err := s.stripeRequest(ctx, http.MethodGet, "/subscriptions/"+subscriptionID, nil, &subscription); err != nil {
		return err
	}

	if subscription.Customer == "" {
		subscription.Customer = customerID
	}
	return s.syncSubscriptionFromStripe(ctx, userID, subscription)
}

func (s *Service) syncSubscriptionFromStripe(ctx context.Context, userID string, subscription stripeSubscription) error {
	if subscription.Customer == "" {
		return fmt.Errorf("stripe subscription %s missing customer", subscription.ID)
	}
	if err := s.store.UpsertBillingCustomer(ctx, userID, subscription.Customer); err != nil {
		return err
	}

	sub := model.Subscription{
		UserID:               userID,
		StripeCustomerID:     subscription.Customer,
		StripeSubscriptionID: subscription.ID,
		Status:               subscription.Status,
		CancelAtPeriodEnd:    subscription.CancelAtPeriodEnd,
	}
	if len(subscription.Items.Data) > 0 {
		sub.StripePriceID = subscription.Items.Data[0].Price.ID
	}
	if subscription.CurrentPeriodStart > 0 {
		sub.CurrentPeriodStart = time.Unix(subscription.CurrentPeriodStart, 0).UTC().Format(time.RFC3339)
	}
	if subscription.CurrentPeriodEnd > 0 {
		sub.CurrentPeriodEnd = time.Unix(subscription.CurrentPeriodEnd, 0).UTC().Format(time.RFC3339)
	}

	_, err := s.store.UpsertSubscription(ctx, sub)
	return err
}

func (s *Service) stripeRequest(ctx context.Context, method, resource string, form neturl.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, stripeAPIBaseURL+resource, body)
	if err != nil {
		return fmt.Errorf("build stripe request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.billing.StripeSecretKey)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("stripe request: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read stripe response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("stripe %s %s: %s", method, resource, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decode stripe response: %w", err)
	}
	return nil
}

func (s *Service) absoluteURL(relativePath string) (string, error) {
	parsed, err := neturl.Parse(s.billing.AppBaseURL)
	if err != nil {
		return "", fmt.Errorf("parse APP_BASE_URL: %w", err)
	}
	ref, err := neturl.Parse(relativePath)
	if err != nil {
		return "", fmt.Errorf("parse relative URL: %w", err)
	}
	return parsed.ResolveReference(ref).String(), nil
}

func (s *Service) billingRedirectURL(returnTo string, params map[string]string) (string, error) {
	target, err := s.absoluteURL(returnTo)
	if err != nil {
		return "", err
	}

	u, err := neturl.Parse(target)
	if err != nil {
		return "", fmt.Errorf("parse redirect URL: %w", err)
	}

	query := u.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func buildCheckoutSuccessURL(baseURL, returnTo string) (string, error) {
	u, err := neturl.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse success URL: %w", err)
	}
	query := u.Query()
	query.Set("billing", "success")
	query.Set("return_to", returnTo)
	u.RawQuery = query.Encode()
	return u.String() + "&session_id={CHECKOUT_SESSION_ID}", nil
}

func verifyStripeSignature(payload []byte, signatureHeader, secret string) (*stripeEvent, error) {
	timestamp, signatures, err := parseStripeSignatureHeader(signatureHeader)
	if err != nil {
		return nil, err
	}

	signedPayload := strconv.FormatInt(timestamp, 10) + "." + string(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write([]byte(signedPayload)); err != nil {
		return nil, err
	}
	expected := hex.EncodeToString(mac.Sum(nil))

	match := false
	for _, signature := range signatures {
		if hmac.Equal([]byte(signature), []byte(expected)) {
			match = true
			break
		}
	}
	if !match {
		return nil, fmt.Errorf("stripe signature verification failed")
	}

	now := time.Now()
	eventTime := time.Unix(timestamp, 0)
	if now.Sub(eventTime) > stripeWebhookTolerance || eventTime.Sub(now) > stripeWebhookTolerance {
		return nil, fmt.Errorf("stripe webhook timestamp outside tolerance")
	}

	var event stripeEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("parse stripe event: %w", err)
	}
	if event.ID == "" {
		return nil, fmt.Errorf("stripe event missing id")
	}
	return &event, nil
}

func parseStripeSignatureHeader(header string) (int64, []string, error) {
	parts := strings.Split(header, ",")
	var timestamp int64
	var signatures []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch key {
		case "t":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, nil, fmt.Errorf("invalid stripe timestamp")
			}
			timestamp = parsed
		case "v1":
			signatures = append(signatures, value)
		}
	}

	if timestamp == 0 || len(signatures) == 0 {
		return 0, nil, fmt.Errorf("invalid stripe signature header")
	}
	return timestamp, signatures, nil
}
