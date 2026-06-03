// Package auth implements `wilco login`: a browser-based, loopback OAuth flow
// (RFC 8252 style) that provisions a per-machine agent token without the user
// copy-pasting anything.
//
// The CLI holds no Wilco session, so it can't mint a token directly. Instead it
// opens the web app (which IS logged in), which mints a single-use CODE and
// redirects the browser to a loopback URL the CLI is listening on. The CLI then
// exchanges that code over HTTPS for the real token. The browser never sees the
// token; the loopback only ever carries the short-lived code.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/withwilco/wilco-agent/internal/config"
)

const loginTimeout = 5 * time.Minute

type tokenResponse struct {
	Token        string   `json:"token"`
	AgentID      string   `json:"agent_id"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
	ServerWSURL  string   `json:"server_ws_url"`
}

// Login runs the full browser handshake and returns a populated, saved Config.
// `name` is the human label for this machine (e.g. the hostname).
func Login(name string) (*config.Config, error) {
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	// Bind loopback on an OS-chosen free port BEFORE opening the browser, so the
	// redirect target exists by the time the browser follows it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not start local listener: %w", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			errCh <- fmt.Errorf("state mismatch on callback — aborting for safety")
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- fmt.Errorf("no code in callback")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(successHTML))
		codeCh <- code
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	authURL := fmt.Sprintf("%s/cli-auth?port=%d&state=%s&name=%s",
		config.AppBase(), port, url.QueryEscape(state), url.QueryEscape(name))

	fmt.Println("Opening your browser to sign in to Wilco…")
	fmt.Printf("If it doesn't open, paste this URL:\n  %s\n\n", authURL)
	_ = openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err = <-errCh:
		return nil, err
	case <-time.After(loginTimeout):
		return nil, fmt.Errorf("timed out waiting for browser sign-in")
	}

	// Exchange the one-time code for the real token, directly over HTTPS.
	resp, err := exchangeCode(code)
	if err != nil {
		return nil, err
	}

	cfg := &config.Config{
		ServerWSURL:       resp.ServerWSURL,
		Token:             resp.Token,
		AgentID:           firstNonEmpty(resp.AgentID, name),
		Capabilities:      resp.Capabilities,
		CleanupAfterBuild: true,
	}
	cfg.WithDefaults()
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("logged in but could not save config: %w", err)
	}
	return cfg, nil
}

func exchangeCode(code string) (*tokenResponse, error) {
	body, _ := json.Marshal(map[string]string{"code": code})
	req, err := http.NewRequest(http.MethodPost,
		config.APIBase()+"/api/agents/cli/token", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not reach Wilco to finish sign-in: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (HTTP %d) — try `wilco login` again", res.StatusCode)
	}
	var tr tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("could not parse server response: %w", err)
	}
	if tr.Token == "" {
		return nil, fmt.Errorf("server returned an empty token")
	}
	return &tr, nil
}

func randomState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func openBrowser(u string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", u).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	default:
		return exec.Command("xdg-open", u).Start()
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

const successHTML = `<!doctype html>
<html><head><meta charset="utf-8"><title>Wilco — connected</title>
<style>
 body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
   background:#0f172a;color:#e2e8f0;display:flex;height:100vh;margin:0;
   align-items:center;justify-content:center}
 .card{text-align:center;max-width:420px;padding:40px}
 h1{font-size:22px;margin:16px 0 8px}
 p{color:#94a3b8;font-size:14px;line-height:1.5}
 .check{font-size:48px}
</style></head>
<body><div class="card">
 <div class="check">✅</div>
 <h1>Your Mac is connected</h1>
 <p>You can close this tab and return to your terminal — the Wilco agent is ready.</p>
</div></body></html>`
