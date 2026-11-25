package panel

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/jub0t/mechon/internal/auth"
	"github.com/jub0t/mechon/internal/db"
)

// Public registration and period-based subscriptions.
//
// A new account gets a user row plus a trial subscription on one plan. The subscription carries
// current_period_end, and a sweeper in the panel suspends anything past its end, which stops that
// account's bots. Nothing here takes payment: a billing system (or an admin) extends the period by
// writing current_period_end, which is what the existing subscription API and webhooks are for.

var (
	errSignupClosed = &apiError{http.StatusForbidden, "signup_closed", "Registration is closed on this host."}
	errEmailTaken   = &apiError{http.StatusConflict, "email_taken", "An account with that email already exists."}
)

const maxNameLen = 80

// signup creates an account from the public form, starts its trial and signs it in.
func (s *Server) signup(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SignupOpen {
		writeError(w, r, errSignupClosed)
		return
	}
	var in struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		// A marketing site can send the plan it sold; it is only honoured when the slug exists.
		PlanSlug string `json:"planSlug"`
	}
	if err := decode(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}

	email := strings.ToLower(strings.TrimSpace(in.Email))
	name := truncate(strings.TrimSpace(in.Name), maxNameLen)
	if len(email) > 254 || !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		writeError(w, r, errBadRequest("Enter a valid email address."))
		return
	}
	if name == "" {
		name = email[:strings.Index(email, "@")]
	}
	if n := len(in.Password); n < auth.MinPasswordLen || n > auth.MaxPasswordLen {
		writeError(w, r, errBadRequest("Password must be at least 10 characters."))
		return
	}

	now := s.now()
	if !s.signupByIP.allow(s.clientIP(r), now) {
		writeError(w, r, errRateLimited)
		return
	}

	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	user, err := s.q.CreateUser(r.Context(), db.CreateUserParams{
		Email: email, Name: name, PasswordHash: &hash, Role: db.UserRoleUser,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, r, errEmailTaken)
			return
		}
		writeError(w, r, err)
		return
	}

	if _, err := s.startTrial(r.Context(), user.ID, in.PlanSlug); err != nil {
		// The account exists and can sign in; without a plan it simply cannot deploy yet.
		slog.Error("signup: start trial", "user", user.Email, "err", err)
	}

	token := auth.NewToken()
	exp := now.Add(sessionTTL)
	if err := s.q.CreateSession(r.Context(), db.CreateSessionParams{
		TokenHash: auth.HashToken(token),
		UserID:    user.ID,
		ExpiresAt: exp,
		Ip:        s.clientIP(r),
		UserAgent: truncate(r.UserAgent(), 512),
	}); err != nil {
		writeError(w, r, err)
		return
	}
	s.setCookie(w, token, exp)
	slog.Info("account created", "email", user.Email, "via", "signup")
	writeJSON(w, http.StatusCreated, toUserJSON(user))
}

// startTrial gives a new account a subscription. The plan is, in order: the slug the caller asked
// for, the configured MECHON_TRIAL_PLAN, then the cheapest live plan preferring one with trial days.
// A plan with trial_days = 0 yields a subscription with no end, which never expires.
func (s *Server) startTrial(ctx context.Context, userID uuid.UUID, wantSlug string) (uuid.UUID, error) {
	type candidate struct {
		id        uuid.UUID
		trialDays int
	}
	lookup := func(query string, arg any) (candidate, error) {
		var c candidate
		var err error
		if arg == nil {
			err = s.pool.QueryRow(ctx, query).Scan(&c.id, &c.trialDays)
		} else {
			err = s.pool.QueryRow(ctx, query, arg).Scan(&c.id, &c.trialDays)
		}
		return c, err
	}

	pick, err := lookup(`SELECT id, trial_days FROM plans
		WHERE archived_at IS NULL AND slug = $1 LIMIT 1`, firstNonEmpty(wantSlug, s.cfg.TrialPlan))
	if err != nil {
		pick, err = lookup(`SELECT id, trial_days FROM plans
			WHERE archived_at IS NULL
			ORDER BY (trial_days > 0) DESC, price_cents, memory_mb
			LIMIT 1`, nil)
		if err != nil {
			return uuid.Nil, err
		}
	}

	var end *time.Time
	if pick.trialDays > 0 {
		e := s.now().Add(time.Duration(pick.trialDays) * 24 * time.Hour)
		end = &e
	}
	var sub uuid.UUID
	err = s.pool.QueryRow(ctx, `INSERT INTO subscriptions (user_id, plan_id, trial, current_period_end)
		VALUES ($1, $2, true, $3) RETURNING id`, userID, pick.id, end).Scan(&sub)
	return sub, err
}

// setPlanBilling writes the billing columns (price, interval, trial days) that the generated plan
// queries do not cover. Nil fields are left untouched, so a limits-only update keeps billing.
func (s *Server) setPlanBilling(ctx context.Context, id uuid.UUID, price *int, interval *string, trialDays *int) error {
	if price == nil && interval == nil && trialDays == nil {
		return nil
	}
	if price != nil && *price < 0 {
		return errors.New("price must not be negative")
	}
	if interval != nil && *interval != "week" && *interval != "month" {
		return errors.New("interval must be week or month")
	}
	if trialDays != nil && (*trialDays < 0 || *trialDays > 90) {
		return errors.New("trial days must be 0 to 90")
	}
	_, err := s.pool.Exec(ctx, `UPDATE plans SET
			price_cents = COALESCE($2, price_cents),
			interval    = COALESCE($3, interval),
			trial_days  = COALESCE($4, trial_days)
		WHERE id = $1`, id, price, interval, trialDays)
	return err
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// StartBilling sweeps expired periods until ctx is done. It runs in the panel because a period end
// is data, not a scheduled external event.
func (s *Server) StartBilling(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				n, err := s.expireSubscriptions(ctx)
				if err != nil {
					slog.Error("billing: expire subscriptions", "err", err)
					continue
				}
				if n > 0 {
					slog.Info("billing: expired subscriptions", "count", n)
				}
			}
		}
	}()
}

// expireSubscriptions suspends subscriptions whose period has ended and stops their bots. It is
// idempotent: only rows still marked active are touched.
func (s *Server) expireSubscriptions(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE subscriptions SET status = 'suspended'
		WHERE status = 'active' AND current_period_end IS NOT NULL AND current_period_end <= now()
		RETURNING id`)
	if err != nil {
		return 0, err
	}
	var expired []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		expired = append(expired, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, id := range expired {
		bots, err := s.q.SetBotsDesiredForSubscription(ctx, db.SetBotsDesiredForSubscriptionParams{
			SubscriptionID: id, DesiredState: db.BotDesiredStateStopped,
		})
		if err != nil {
			return len(expired), err
		}
		for _, b := range bots {
			s.hub.pushBot(context.WithoutCancel(ctx), b.ID)
		}
		s.hub.pushSubscription(context.WithoutCancel(ctx), id)
		s.emit(ctx, "subscription.suspended", map[string]any{"id": id, "reason": "period_ended"})
	}
	return len(expired), nil
}
