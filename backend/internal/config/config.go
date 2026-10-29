// Package config loads runtime configuration from environment variables.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime settings.
type Config struct {
	ListenAddr string

	TalosConfigPath string
	TalosContext    string
	KubeconfigPath  string

	AdminUser           string
	AdminPassword       string
	AdminPasswordBcrypt string
	SessionSecret       []byte
	SessionTTL          time.Duration
	CookieSecure        bool

	DataDir string

	// GitHubToken is optional and only used to raise the GitHub API rate limit
	// when checking for new Talos releases.
	GitHubToken string
}

// Load reads configuration from the environment.
func Load() *Config {
	c := &Config{
		ListenAddr:          env("LISTEN_ADDR", ":8080"),
		TalosConfigPath:     env("TALOSCONFIG", "/config/talosconfig"),
		TalosContext:        os.Getenv("TALOS_CONTEXT"),
		KubeconfigPath:      env("KUBECONFIG", "/config/kubeconfig"),
		AdminUser:           env("ADMIN_USER", "admin"),
		AdminPassword:       os.Getenv("ADMIN_PASSWORD"),
		AdminPasswordBcrypt: os.Getenv("ADMIN_PASSWORD_BCRYPT"),
		SessionTTL:          envDuration("SESSION_TTL", 12*time.Hour),
		CookieSecure:        envBool("COOKIE_SECURE", false),
		DataDir:             env("DATA_DIR", "/data"),
		GitHubToken:         os.Getenv("GITHUB_TOKEN"),
	}

	if s := os.Getenv("SESSION_SECRET"); s != "" {
		c.SessionSecret = []byte(s)
	} else {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		c.SessionSecret = []byte(hex.EncodeToString(b))
		slog.Warn("SESSION_SECRET not set; generated a random one (sessions will not survive restarts)")
	}

	return c
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}

	return def
}

func envBool(k string, def bool) bool {
	if v := os.Getenv(k); v != "" {
		b, err := strconv.ParseBool(v)
		if err == nil {
			return b
		}
	}

	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}

	return def
}
