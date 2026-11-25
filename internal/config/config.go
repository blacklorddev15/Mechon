// Package config reads the panel's settings from the environment. There is no config file.
//
// Every setting has a PaaS-friendly fallback so the panel can run on Heroku (or any platform that
// injects PORT and DATABASE_URL) without extra configuration:
//
//	MECHON_LISTEN      -> 0.0.0.0:$PORT      (Heroku assigns the port)
//	MECHON_DATABASE_URL -> $DATABASE_URL     (set by Heroku Postgres and most add-ons)
//	MECHON_PUBLIC_URL  -> https://$HEROKU_APP_DEFAULT_DOMAIN_NAME
//	MECHON_DATA_DIR    -> /tmp/mechon-data   (a dyno's filesystem is ephemeral)
//	MECHON_TRUST_PROXY -> true               (the Heroku router always sits in front)
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

type Panel struct {
	Listen      string   // MECHON_LISTEN, default ":8080" (or "0.0.0.0:$PORT")
	DatabaseURL string   // MECHON_DATABASE_URL or DATABASE_URL, required
	PublicURL   *url.URL // MECHON_PUBLIC_URL, required: where browsers reach the panel
	TrustProxy  bool     // MECHON_TRUST_PROXY: take the client IP from X-Forwarded-For
	SecretKey   string   // MECHON_SECRET_KEY, required: encrypts bot secrets at rest
	DataDir     string   // MECHON_DATA_DIR, default "./data": uploaded artifacts live here
	OnHeroku    bool     // DYNO is set: running inside a Heroku dyno
	SignupOpen  bool     // MECHON_SIGNUP: "closed" disables public registration, default open
	TrialPlan   string   // MECHON_TRIAL_PLAN: plan slug a new account starts on, default cheapest
}

func LoadPanel() (Panel, error) {
	onHeroku := os.Getenv("DYNO") != ""
	c := Panel{
		Listen:      listenAddr(),
		DatabaseURL: databaseURL(),
		TrustProxy:  trustProxy(onHeroku),
		SecretKey:   os.Getenv("MECHON_SECRET_KEY"),
		DataDir:     dataDir(onHeroku),
		OnHeroku:    onHeroku,
		SignupOpen:  os.Getenv("MECHON_SIGNUP") != "closed",
		TrialPlan:   os.Getenv("MECHON_TRIAL_PLAN"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("MECHON_DATABASE_URL (or DATABASE_URL) is required"))
	}
	if c.SecretKey == "" {
		errs = append(errs, errors.New("MECHON_SECRET_KEY is required (generate one with `mechon keygen`)"))
	}
	raw := publicURL()
	if raw == "" {
		errs = append(errs, errors.New("MECHON_PUBLIC_URL is required (e.g. https://panel.example.com)"))
	} else if u, err := url.Parse(strings.TrimRight(raw, "/")); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("MECHON_PUBLIC_URL %q is not an http(s) URL", raw))
	} else {
		c.PublicURL = u
	}
	return c, errors.Join(errs...)
}

// Origin is the scheme://host[:port] browsers send in the Origin header.
func (c Panel) Origin() string { return c.PublicURL.Scheme + "://" + c.PublicURL.Host }

// SecureCookies is on whenever the panel is served over HTTPS.
func (c Panel) SecureCookies() bool { return c.PublicURL.Scheme == "https" }

// EphemeralData reports whether DataDir lives on a filesystem that is wiped when the process
// restarts, which is the case on Heroku. Uploaded deploy artifacts survive until then.
func (c Panel) EphemeralData() bool { return c.OnHeroku || strings.HasPrefix(c.DataDir, "/tmp/") }

// listenAddr prefers MECHON_LISTEN, then the port a platform assigns through PORT.
func listenAddr() string {
	if v := os.Getenv("MECHON_LISTEN"); v != "" {
		return v
	}
	if p := os.Getenv("PORT"); p != "" {
		return "0.0.0.0:" + p
	}
	return ":8080"
}

// databaseURL prefers MECHON_DATABASE_URL, then DATABASE_URL as set by Heroku Postgres.
func databaseURL() string {
	raw := os.Getenv("MECHON_DATABASE_URL")
	if raw == "" {
		raw = os.Getenv("DATABASE_URL")
	}
	return requireSSL(raw)
}

// requireSSL adds sslmode=require when the URL targets a network host and says nothing about TLS.
// Heroku Postgres and most managed databases reject plaintext connections, and pgx's default
// ("prefer") silently falls back to plaintext instead of failing loudly.
func requireSSL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw // unix-socket form, e.g. postgres:///mechon?host=/var/run/postgresql
	}
	q := u.Query()
	if q.Has("sslmode") {
		return raw
	}
	switch u.Hostname() {
	case "", "localhost", "127.0.0.1", "::1":
		return raw
	}
	q.Set("sslmode", "require")
	u.RawQuery = q.Encode()
	return u.String()
}

// publicURL prefers MECHON_PUBLIC_URL, then the domain Heroku gives the app.
func publicURL() string {
	if v := os.Getenv("MECHON_PUBLIC_URL"); v != "" {
		return v
	}
	if d := os.Getenv("HEROKU_APP_DEFAULT_DOMAIN_NAME"); d != "" {
		return "https://" + d
	}
	if n := os.Getenv("HEROKU_APP_NAME"); n != "" {
		return "https://" + n + ".herokuapp.com"
	}
	return ""
}

// dataDir: uploaded artifacts live here. A dyno has no persistent disk, so /tmp is the only
// writable place; it is emptied on every restart and dyno cycle.
func dataDir(onHeroku bool) string {
	if v := os.Getenv("MECHON_DATA_DIR"); v != "" {
		return v
	}
	if onHeroku {
		return "/tmp/mechon-data"
	}
	return "./data"
}

// trustProxy defaults to true on Heroku, where the router always forwards the real client IP.
func trustProxy(onHeroku bool) bool {
	if v := os.Getenv("MECHON_TRUST_PROXY"); v != "" {
		return v == "true"
	}
	return onHeroku
}
