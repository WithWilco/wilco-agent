// Package config persists the Wilco agent's settings — most importantly the
// per-machine enrollment token obtained from `wilco login`.
//
// The token is the same kind of secret the server-side WebSocket agent already
// authenticates with: high entropy, stored only as a SHA-256 hash on the server,
// and tied to one user. Here it lives in a 0600 file under the user's config
// directory so other local users can't read it.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultAPIBase = "https://api.withwilco.com"
	defaultAppBase = "https://withwilco.com"
	dirName        = "wilco"
	fileName       = "config.json"
)

// Config is what we persist between runs.
type Config struct {
	// ServerWSURL is the wss:// agent endpoint, handed back by the token
	// exchange so the CLI never has to be told where to connect.
	ServerWSURL string `json:"server_ws_url"`
	// Token is the per-machine enrollment secret. Empty means "not logged in".
	Token string `json:"token"`
	// AgentID is a human label sent on register (defaults to the hostname).
	AgentID           string   `json:"agent_id"`
	Capabilities      []string `json:"capabilities"`
	FastlaneLanes     []string `json:"fastlane_lanes"`
	WorkDir           string   `json:"work_dir"`
	CleanupAfterBuild bool     `json:"cleanup_after_build"`
	// TLSPinSHA256 optionally pins the server's leaf certificate (advanced).
	TLSPinSHA256 []string `json:"tls_pin_sha256,omitempty"`
}

// LoggedIn reports whether we hold an enrollment token.
func (c *Config) LoggedIn() bool { return strings.TrimSpace(c.Token) != "" }

// APIBase is the https base for REST calls. Overridable for dev via WILCO_API_URL.
func APIBase() string {
	if v := strings.TrimSpace(os.Getenv("WILCO_API_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultAPIBase
}

// AppBase is the https base for the web app (where /cli-auth lives).
// Overridable for dev via WILCO_APP_URL.
func AppBase() string {
	if v := strings.TrimSpace(os.Getenv("WILCO_APP_URL")); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultAppBase
}

// Dir is the directory the config file lives in (created on Save).
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, dirName), nil
}

// Path is the absolute path to the config file.
func Path() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, fileName), nil
}

// Load reads the config, returning a zero-value Config (not an error) when no
// file exists yet — a first run is not a failure.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("config at %s is corrupt: %w", p, err)
	}
	return &c, nil
}

// Save writes the config with restrictive permissions (0700 dir, 0600 file) so
// the token at rest is not world-readable.
func (c *Config) Save() error {
	d, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return err
	}
	p := filepath.Join(d, fileName)
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file then rename so a crash can't leave a half-written
	// config behind.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Clear removes the stored config (used by `wilco logout`).
func Clear() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WithDefaults fills in sensible values for anything the token exchange didn't
// set, so a freshly logged-in config is immediately runnable.
func (c *Config) WithDefaults() *Config {
	if len(c.Capabilities) == 0 {
		c.Capabilities = []string{"ios"}
	}
	if len(c.FastlaneLanes) == 0 {
		c.FastlaneLanes = []string{"beta", "release"}
	}
	if strings.TrimSpace(c.WorkDir) == "" {
		c.WorkDir = "~/wilco-builds"
	}
	if strings.TrimSpace(c.AgentID) == "" {
		if h, err := os.Hostname(); err == nil {
			c.AgentID = h
		} else {
			c.AgentID = "my-mac"
		}
	}
	return c
}
