// Package service manages the macOS LaunchAgent that keeps the Wilco agent
// running in the background and restarts it on login or crash — the "run always"
// option offered during setup.
package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const label = "com.wilco.agent"

// plistPath is ~/Library/LaunchAgents/com.wilco.agent.plist
func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func logPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "wilco"), nil
}

// Install writes the LaunchAgent plist (pointing at this binary's `start`
// command) and loads it so the agent runs now and on every login.
func Install() error {
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	pl, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pl), 0o755); err != nil {
		return err
	}

	logs, err := logPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(logs, 0o755); err != nil {
		return err
	}
	stdout := filepath.Join(logs, "agent.out.log")
	stderr := filepath.Join(logs, "agent.err.log")

	if err := os.WriteFile(pl, []byte(renderPlist(exePath, stdout, stderr)), 0o644); err != nil {
		return err
	}

	// Reload cleanly: bootout an old instance (ignore errors), then bootstrap.
	target := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", target+"/"+label).Run()
	if out, err := exec.Command("launchctl", "bootstrap", target, pl).CombinedOutput(); err != nil {
		// Fall back to the older load syntax on systems where bootstrap fails.
		if out2, err2 := exec.Command("launchctl", "load", "-w", pl).CombinedOutput(); err2 != nil {
			return fmt.Errorf("could not load LaunchAgent: %s / %s", string(out), string(out2))
		}
	}
	return nil
}

// Uninstall stops the LaunchAgent and removes its plist.
func Uninstall() error {
	pl, err := plistPath()
	if err != nil {
		return err
	}
	target := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", target+"/"+label).Run()
	_ = exec.Command("launchctl", "unload", pl).Run()
	if err := os.Remove(pl); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Installed reports whether the LaunchAgent plist exists.
func Installed() bool {
	pl, err := plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(pl)
	return err == nil
}

// Status returns a human-readable run state from launchctl.
func Status() string {
	if !Installed() {
		return "not installed"
	}
	out, err := exec.Command("launchctl", "list").Output()
	if err != nil {
		return "installed (state unknown)"
	}
	if strings.Contains(string(out), label) {
		return "installed and loaded"
	}
	return "installed (not loaded)"
}

func renderPlist(exePath, stdout, stderr string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>start</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>ThrottleInterval</key>
    <integer>10</integer>
    <key>StandardOutPath</key>
    <string>%s</string>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
</dict>
</plist>
`, label, exePath, stdout, stderr)
}
