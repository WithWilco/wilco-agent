// Command wilco is the Wilco build agent CLI. Install it with Homebrew and run
// `wilco` — it checks your toolchain, signs you in through the browser, and runs
// the agent (optionally always, in the background).
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/withwilco/wilco-agent/internal/agent"
	"github.com/withwilco/wilco-agent/internal/auth"
	"github.com/withwilco/wilco-agent/internal/config"
	"github.com/withwilco/wilco-agent/internal/doctor"
	"github.com/withwilco/wilco-agent/internal/service"
)

const version = "0.1.0"

func main() {
	args := os.Args[1:]
	cmd := "setup"
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}

	var err error
	switch cmd {
	case "setup", "run":
		err = cmdSetup()
	case "login":
		err = cmdLogin()
	case "logout":
		err = cmdLogout()
	case "doctor":
		doctor.Run(true)
	case "start":
		err = cmdStart()
	case "status":
		err = cmdStatus()
	case "service":
		err = cmdService(args)
	case "version", "--version", "-v":
		fmt.Printf("wilco %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printHelp()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// cmdSetup is the guided first-run flow: check tools → sign in → choose how to run.
func cmdSetup() error {
	fmt.Println("Wilco build agent — setup")
	fmt.Println(strings.Repeat("─", 40))
	fmt.Println("\nChecking your build toolchain:")
	doctor.Run(true)

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if !cfg.LoggedIn() {
		fmt.Println("\nLet's connect this Mac to your Wilco account.")
		name, _ := os.Hostname()
		cfg, err = auth.Login(name)
		if err != nil {
			return err
		}
		fmt.Printf("\n✅ Connected as agent %q.\n", cfg.AgentID)
	} else {
		fmt.Printf("\n✓ Already connected as agent %q.\n", cfg.AgentID)
	}

	fmt.Println()
	if ask("Keep the agent running always (start at login, restart on crash)? [Y/n] ") {
		if err := service.Install(); err != nil {
			return fmt.Errorf("could not set up the background service: %w", err)
		}
		fmt.Println("\n✅ Done. The agent is running in the background and will start at login.")
		fmt.Println("   Logs: ~/Library/Logs/wilco/   Manage: wilco service status | uninstall")
		return nil
	}

	fmt.Println("\nStarting the agent in this terminal. Press Ctrl-C to stop.")
	fmt.Println(strings.Repeat("─", 40))
	return cmdStart()
}

func cmdLogin() error {
	name, _ := os.Hostname()
	cfg, err := auth.Login(name)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Connected as agent %q.\n", cfg.AgentID)
	return nil
}

func cmdLogout() error {
	if service.Installed() {
		_ = service.Uninstall()
		fmt.Println("Stopped the background service.")
	}
	if err := config.Clear(); err != nil {
		return err
	}
	fmt.Println("Logged out. Run `wilco login` to reconnect.")
	return nil
}

func cmdStart() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.WithDefaults()
	if !cfg.LoggedIn() {
		return fmt.Errorf("not logged in — run `wilco login` first")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return agent.Run(ctx, cfg)
}

func cmdStatus() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	p, _ := config.Path()
	fmt.Printf("Config:    %s\n", p)
	if cfg.LoggedIn() {
		fmt.Printf("Account:   connected (agent %q)\n", cfg.AgentID)
		fmt.Printf("Server:    %s\n", cfg.ServerWSURL)
	} else {
		fmt.Println("Account:   not connected — run `wilco login`")
	}
	fmt.Printf("Service:   %s\n", service.Status())
	return nil
}

func cmdService(args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "install":
		if err := service.Install(); err != nil {
			return err
		}
		fmt.Println("✅ Background service installed and started.")
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			return err
		}
		fmt.Println("Background service removed.")
	case "status":
		fmt.Printf("Service: %s\n", service.Status())
	default:
		return fmt.Errorf("unknown service command %q (use install | uninstall | status)", sub)
	}
	return nil
}

func ask(q string) bool {
	fmt.Print(q)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}

func printHelp() {
	fmt.Print(`wilco — Wilco build agent

Usage:
  wilco                 Guided setup: check tools, sign in, run the agent
  wilco login           Connect this Mac to your Wilco account (opens browser)
  wilco logout          Disconnect and stop the background service
  wilco doctor          Check Xcode / Command Line Tools / Fastlane
  wilco start           Run the agent in the foreground
  wilco status          Show login and service status
  wilco service ...     install | uninstall | status (run-always background service)
  wilco version         Print the version

Environment overrides (advanced):
  WILCO_API_URL         API base (default https://api.withwilco.com)
  WILCO_APP_URL         Web app base (default https://withwilco.com)
`)
}
