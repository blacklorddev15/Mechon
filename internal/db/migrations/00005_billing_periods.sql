-- +goose Up

-- Plans are sold per period now: a price, a billing interval, and how many trial days a new
-- account gets on that plan. Defaults keep existing rows valid (free, monthly, no trial).
ALTER TABLE plans
    ADD COLUMN price_cents int  NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
    ADD COLUMN interval    text NOT NULL DEFAULT 'month' CHECK (interval IN ('week', 'month')),
    ADD COLUMN trial_days  int  NOT NULL DEFAULT 0 CHECK (trial_days BETWEEN 0 AND 90);

-- When the current paid (or trial) period ends. NULL means "no end", which is how subscriptions
-- created by a billing system or by hand behave.
ALTER TABLE subscriptions
    ADD COLUMN current_period_end timestamptz,
    ADD COLUMN trial              bool NOT NULL DEFAULT false;

CREATE INDEX subscriptions_period_end_idx
    ON subscriptions (current_period_end)
    WHERE status = 'active' AND current_period_end IS NOT NULL;

-- +goose Down

DROP INDEX subscriptions_period_end_idx;

ALTER TABLE subscriptions
    DROP COLUMN trial,
    DROP COLUMN current_period_end;

ALTER TABLE plans
    DROP COLUMN trial_days,
    DROP COLUMN interval,
    DROP COLUMN price_cents;
