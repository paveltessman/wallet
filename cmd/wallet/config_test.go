package main

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func validEnv() map[string]string {
	return map[string]string{
		"HTTP_PORT":          "8080",
		"POSTGRES_HOST":      "db",
		"POSTGRES_PORT":      "5432",
		"POSTGRES_USER":      "wallet",
		"POSTGRES_PASSWORD":  "wallet",
		"POSTGRES_DB":        "wallet",
		"DB_MAX_CONNS":       "50",
		"DB_ACQUIRE_TIMEOUT": "2s",
	}
}

func getenv(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func TestLoadConfig(t *testing.T) {
	t.Run("reads every variable", func(t *testing.T) {
		cfg, err := loadConfig(getenv(validEnv()))
		if err != nil {
			t.Fatalf("loadConfig: %v", err)
		}

		want := config{
			HTTPPort:         8080,
			PostgresHost:     "db",
			PostgresPort:     5432,
			PostgresUser:     "wallet",
			PostgresPassword: "wallet",
			PostgresDB:       "wallet",
			DBMaxConns:       50,
			DBAcquireTimeout: 2 * time.Second,
		}
		if cfg != want {
			t.Errorf("config = %+v, want %+v", cfg, want)
		}
	})

	t.Run("reports every missing variable", func(t *testing.T) {
		_, err := loadConfig(getenv(nil))
		if err == nil {
			t.Fatal("err = nil, want an error")
		}
		for name := range validEnv() {
			if !strings.Contains(err.Error(), name+" is not set") {
				t.Errorf("err = %q, want it to name %s", err, name)
			}
		}
	})

	tests := []struct {
		name  string
		value string
	}{
		{"HTTP_PORT", "http"},
		{"HTTP_PORT", "0"},
		{"HTTP_PORT", "65536"},
		{"POSTGRES_PORT", "-1"},
		{"DB_MAX_CONNS", "0"},
		{"DB_MAX_CONNS", "2147483648"},
		{"DB_MAX_CONNS", "1.5"},
		{"DB_ACQUIRE_TIMEOUT", "2"},
		{"DB_ACQUIRE_TIMEOUT", "0s"},
		{"DB_ACQUIRE_TIMEOUT", "-1s"},
	}
	for _, tt := range tests {
		t.Run("rejects "+tt.name+"="+tt.value, func(t *testing.T) {
			env := validEnv()
			env[tt.name] = tt.value

			_, err := loadConfig(getenv(env))
			if err == nil {
				t.Fatal("err = nil, want an error")
			}
			if !strings.Contains(err.Error(), tt.name) || !strings.Contains(err.Error(), `"`+tt.value+`"`) {
				t.Errorf("err = %q, want it to name %s and the value %q", err, tt.name, tt.value)
			}
		})
	}
}

func TestDatabaseURL(t *testing.T) {
	cfg, err := loadConfig(getenv(validEnv()))
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	cfg.PostgresPassword = "p@ss/word?#"

	u, err := url.Parse(cfg.databaseURL())
	if err != nil {
		t.Fatalf("parse %q: %v", cfg.databaseURL(), err)
	}

	if got, _ := u.User.Password(); got != cfg.PostgresPassword {
		t.Errorf("password = %q, want %q", got, cfg.PostgresPassword)
	}
	if u.User.Username() != "wallet" {
		t.Errorf("user = %q, want wallet", u.User.Username())
	}
	if u.Host != "db:5432" {
		t.Errorf("host = %q, want db:5432", u.Host)
	}
	if u.Path != "/wallet" {
		t.Errorf("path = %q, want /wallet", u.Path)
	}
	if u.Query().Get("sslmode") != "disable" {
		t.Errorf("sslmode = %q, want disable", u.Query().Get("sslmode"))
	}
}
