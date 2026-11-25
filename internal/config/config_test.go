package config

import (
	"strings"
	"testing"
)

// clearEnv unsets every variable LoadPanel reads, so a test never inherits the shell's settings.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MECHON_LISTEN", "PORT", "MECHON_DATABASE_URL", "DATABASE_URL",
		"MECHON_PUBLIC_URL", "HEROKU_APP_NAME", "HEROKU_APP_DEFAULT_DOMAIN_NAME",
		"MECHON_SECRET_KEY", "MECHON_DATA_DIR", "MECHON_TRUST_PROXY", "DYNO",
	} {
		t.Setenv(k, "")
	}
}

func TestListenPrefersMechonThenPort(t *testing.T) {
	clearEnv(t)
	if got := listenAddr(); got != ":8080" {
		t.Fatalf("no env: got %q, want :8080", got)
	}
	t.Setenv("PORT", "1234")
	if got := listenAddr(); got != "0.0.0.0:1234" {
		t.Fatalf("PORT only: got %q, want 0.0.0.0:1234", got)
	}
	t.Setenv("MECHON_LISTEN", "127.0.0.1:9000")
	if got := listenAddr(); got != "127.0.0.1:9000" {
		t.Fatalf("explicit MECHON_LISTEN must win: got %q", got)
	}
}

func TestDatabaseURLFallsBackToDatabaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/mechon")
	if got := databaseURL(); got != "postgres://u:p@db.internal:5432/mechon?sslmode=require" {
		t.Fatalf("got %q", got)
	}
	t.Setenv("MECHON_DATABASE_URL", "postgres://u:p@explicit:5432/mechon")
	if got := databaseURL(); !strings.Contains(got, "explicit") {
		t.Fatalf("MECHON_DATABASE_URL must win, got %q", got)
	}
}

func TestRequireSSL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"postgres://u:p@db.internal:5432/mechon", "postgres://u:p@db.internal:5432/mechon?sslmode=require"},
		{"postgres://u:p@db.internal/mechon?sslmode=disable", "postgres://u:p@db.internal/mechon?sslmode=disable"},
		{"postgres://u:p@localhost:5432/mechon", "postgres://u:p@localhost:5432/mechon"},
		{"postgres:///mechon?host=/var/run/postgresql", "postgres:///mechon?host=/var/run/postgresql"},
		{"postgres://u:p@db:5432/mechon?pool_max_conns=5", "postgres://u:p@db:5432/mechon?pool_max_conns=5&sslmode=require"},
	}
	for _, c := range cases {
		if got := requireSSL(c.in); got != c.want {
			t.Errorf("requireSSL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPublicURLPrefersExplicitThenHerokuDomain(t *testing.T) {
	clearEnv(t)
	if got := publicURL(); got != "" {
		t.Fatalf("no env: got %q", got)
	}
	t.Setenv("HEROKU_APP_NAME", "my-panel")
	if got := publicURL(); got != "https://my-panel.herokuapp.com" {
		t.Fatalf("HEROKU_APP_NAME: got %q", got)
	}
	t.Setenv("HEROKU_APP_DEFAULT_DOMAIN_NAME", "my-panel-1a2b.herokuapp.com")
	if got := publicURL(); got != "https://my-panel-1a2b.herokuapp.com" {
		t.Fatalf("domain var must win over the name: got %q", got)
	}
	t.Setenv("MECHON_PUBLIC_URL", "https://panel.example.com")
	if got := publicURL(); got != "https://panel.example.com" {
		t.Fatalf("explicit MECHON_PUBLIC_URL must win: got %q", got)
	}
}

func TestDataDirOnHerokuIsTmp(t *testing.T) {
	clearEnv(t)
	if got := dataDir(false); got != "./data" {
		t.Fatalf("unmanaged host: got %q", got)
	}
	if got := dataDir(true); got != "/tmp/mechon-data" {
		t.Fatalf("heroku: got %q", got)
	}
	t.Setenv("MECHON_DATA_DIR", "/var/lib/mechon-panel")
	if got := dataDir(true); got != "/var/lib/mechon-panel" {
		t.Fatalf("explicit MECHON_DATA_DIR must win: got %q", got)
	}
}

func TestTrustProxyDefaultsOnHeroku(t *testing.T) {
	clearEnv(t)
	if trustProxy(false) {
		t.Fatal("trust proxy must default to false off a PaaS")
	}
	if !trustProxy(true) {
		t.Fatal("trust proxy must default to true on Heroku")
	}
	t.Setenv("MECHON_TRUST_PROXY", "false")
	if trustProxy(true) {
		t.Fatal("an explicit false must win")
	}
}

func TestLoadPanelOnHerokuUsesPlatformDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DYNO", "web.1")
	t.Setenv("PORT", "4321")
	t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/mechon")
	t.Setenv("HEROKU_APP_NAME", "my-panel")
	t.Setenv("MECHON_SECRET_KEY", "ZGV2LW9ubHktbWVjaG9uLXNlY3JldC1rZXktMzJieXQ=")

	c, err := LoadPanel()
	if err != nil {
		t.Fatalf("LoadPanel: %v", err)
	}
	if c.Listen != "0.0.0.0:4321" {
		t.Errorf("listen = %q", c.Listen)
	}
	if c.PublicURL.String() != "https://my-panel.herokuapp.com" {
		t.Errorf("public url = %q", c.PublicURL)
	}
	if !c.OnHeroku || !c.EphemeralData() {
		t.Errorf("heroku flags: onHeroku=%v ephemeral=%v", c.OnHeroku, c.EphemeralData())
	}
	if !c.TrustProxy {
		t.Error("trust proxy should be on behind the Heroku router")
	}
	if !c.SecureCookies() {
		t.Error("cookies must be secure on an https public URL")
	}
	if c.Origin() != "https://my-panel.herokuapp.com" {
		t.Errorf("origin = %q", c.Origin())
	}
}

func TestLoadPanelWithoutPlatformFailsLoudly(t *testing.T) {
	clearEnv(t)
	if _, err := LoadPanel(); err == nil {
		t.Fatal("expected an error when nothing is configured")
	} else {
		for _, want := range []string{"MECHON_SECRET_KEY", "MECHON_PUBLIC_URL", "DATABASE_URL"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q should mention %s", err, want)
			}
		}
	}
}
