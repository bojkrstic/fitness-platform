CREATE TABLE IF NOT EXISTS billing_customers (
	user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	stripe_customer_id TEXT NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_subscriptions (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
	stripe_customer_id TEXT NOT NULL REFERENCES billing_customers(stripe_customer_id) ON DELETE CASCADE,
	stripe_subscription_id TEXT NOT NULL UNIQUE,
	stripe_price_id TEXT NOT NULL,
	status TEXT NOT NULL,
	current_period_start TIMESTAMPTZ,
	current_period_end TIMESTAMPTZ,
	cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_subscriptions_status
ON user_subscriptions (status, current_period_end DESC);

CREATE TABLE IF NOT EXISTS stripe_webhook_events (
	event_id TEXT PRIMARY KEY,
	event_type TEXT NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
