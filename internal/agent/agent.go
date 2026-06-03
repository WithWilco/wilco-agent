// Package agent is the build worker: it dials OUT to the Wilco server over a
// persistent wss:// connection, authenticates with the per-machine token, then
// receives build jobs and streams logs back. Nothing listens on this machine, so
// there is no inbound surface to attack — the only way in is to hold the token.
package agent

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/withwilco/wilco-agent/internal/config"
)

// Run connects and serves build jobs forever, reconnecting with backoff. It
// returns only on an unrecoverable configuration error.
func Run(ctx context.Context, cfg *config.Config) error {
	if !cfg.LoggedIn() {
		return fmt.Errorf("not logged in — run `wilco login` first")
	}
	if cfg.ServerWSURL == "" {
		return fmt.Errorf("no server URL in config — run `wilco login` again")
	}

	parsed, err := url.Parse(cfg.ServerWSURL)
	if err != nil {
		return fmt.Errorf("invalid server URL %q: %w", cfg.ServerWSURL, err)
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "wss" && host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("refusing to connect: %s is not secure — use wss:// for any non-local server", cfg.ServerWSURL)
	}

	pins := normalizePins(cfg.TLSPinSHA256)
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 15 * time.Second
	if len(pins) > 0 {
		log.Printf("Certificate pinning enabled (%d pin(s)).", len(pins))
		dialer.TLSClientConfig = &tls.Config{
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return fmt.Errorf("no server certificate presented")
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				fp := hex.EncodeToString(sum[:])
				for _, p := range pins {
					if p == fp {
						return nil
					}
				}
				return fmt.Errorf("server certificate %s is not in the configured pin set", fp)
			},
		}
	}

	machineInfo := buildMachineInfo()
	delay := 5 * time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("Connecting to %s ...", cfg.ServerWSURL)
		if serveOnce(ctx, &dialer, cfg, machineInfo) {
			delay = 5 * time.Second // reset after a clean session
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("Connection lost. Reconnecting in %s...", delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > 60*time.Second {
			delay = 60 * time.Second
		}
	}
}

// serveOnce runs a single connection lifecycle. Returns true if it registered
// successfully (so the caller can reset its backoff).
func serveOnce(ctx context.Context, dialer *websocket.Dialer, cfg *config.Config, machineInfo string) bool {
	conn, _, err := dialer.DialContext(ctx, cfg.ServerWSURL, nil)
	if err != nil {
		log.Printf("Connect failed: %v", err)
		return false
	}
	defer conn.Close()

	register := map[string]any{
		"type":           "register",
		"agent_id":       cfg.AgentID,
		"token":          cfg.Token,
		"capabilities":   cfg.Capabilities,
		"fastlane_lanes": cfg.FastlaneLanes,
		"machine_info":   machineInfo,
	}
	if err := conn.WriteJSON(register); err != nil {
		log.Printf("Could not send registration: %v", err)
		return false
	}

	// Expect a registration result before serving jobs.
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	var resp map[string]any
	if err := conn.ReadJSON(&resp); err != nil {
		log.Printf("No registration response: %v", err)
		return false
	}
	if t, _ := resp["type"].(string); t == "error" {
		log.Printf("Registration refused: %v", resp["detail"])
		return false
	}
	_ = conn.SetReadDeadline(time.Time{}) // clear deadline; builds can take a long time
	log.Printf("✓ Registered as %q. Waiting for build jobs...", cfg.AgentID)

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return true // registered earlier; treat drop as reconnectable
		}
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &head); err != nil {
			continue
		}

		switch head.Type {
		case "build":
			var job Job
			if err := json.Unmarshal(data, &job); err != nil {
				log.Printf("Ignoring malformed build job: %v", err)
				continue
			}
			log.Printf("Received build job #%d (platform=%s, lane=%s, scheme=%s)", job.BuildID, job.Platform, job.FastlaneLane, job.Scheme)
			runJob(ctx, conn, cfg, job)
			log.Printf("Build job #%d completed", job.BuildID)
		case "list_schemes":
			var req SchemeRequest
			if err := json.Unmarshal(data, &req); err != nil {
				log.Printf("Ignoring malformed scheme request: %v", err)
				continue
			}
			log.Printf("Discovering Xcode schemes for %s @ %s", req.RepoURL, req.Branch)
			schemes, err := discoverSchemes(ctx, req, cfg.WorkDir)
			if err != nil {
				log.Printf("Scheme discovery failed: %v", err)
				schemes = []string{}
			}
			_ = conn.WriteJSON(map[string]any{
				"type":       "schemes",
				"request_id": req.RequestID,
				"schemes":    schemes,
			})
		case "ping":
			_ = conn.WriteJSON(map[string]any{"type": "pong"})
		case "heartbeat_ack":
			// nothing to do
		default:
			// ignore unknown messages
		}
	}
}

func runJob(ctx context.Context, conn *websocket.Conn, cfg *config.Config, job Job) {
	sendLog := func(line string) {
		_ = conn.WriteJSON(map[string]any{"type": "log", "build_id": job.BuildID, "line": line})
	}
	res := runBuild(ctx, job, cfg.WorkDir, cfg.CleanupAfterBuild, sendLog)
	_ = conn.WriteJSON(map[string]any{
		"type":               "build_done",
		"build_id":           job.BuildID,
		"exit_code":          res.exitCode,
		"error_summary":      res.errorSummary,
		"artifact_size_bytes": res.artifactBytes,
	})
}

func buildMachineInfo() string {
	host := "unknown"
	if h, err := exec.Command("hostname").Output(); err == nil {
		host = strings.TrimSpace(string(h))
	}
	info := fmt.Sprintf("%s / %s %s / Go agent", host, runtime.GOOS, runtime.GOARCH)
	if out, err := exec.Command("xcodebuild", "-version").Output(); err == nil {
		if line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]; line != "" {
			info += " / " + line
		}
	}
	return info
}

func normalizePins(raw []string) []string {
	var out []string
	for _, p := range raw {
		p = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(p, ":", ""), " ", "")))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
