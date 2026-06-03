// Package doctor checks (and, where safe, fixes) the local toolchain the agent
// needs to build iOS apps: Xcode + command line tools, and Fastlane.
//
// Xcode is multi-gigabyte and gated behind the App Store / Apple ID, so we never
// try to install it automatically — we detect it and tell the user exactly what
// to do. Fastlane is a Homebrew formula, so with the user's consent we install
// it for them.
package doctor

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Result summarizes one check.
type Result struct {
	OK      bool
	Name    string
	Detail  string
	Fixable bool // true if `wilco` can fix it (with consent)
}

// Run performs all checks and prints a report. If `interactive` is true it will
// offer to install missing-but-fixable tools (Fastlane). Returns true if the
// machine is build-ready.
func Run(interactive bool) bool {
	xc := CheckXcode()
	report(xc)
	clt := CheckCommandLineTools()
	report(clt)
	fl := CheckFastlane()
	report(fl)

	if !fl.OK && fl.Fixable && interactive {
		if prompt("Install Fastlane now with Homebrew? [Y/n] ") {
			if err := InstallFastlane(); err != nil {
				fmt.Printf("  ✗ Fastlane install failed: %v\n", err)
			} else {
				fl = CheckFastlane()
				report(fl)
			}
		}
	}

	ready := xc.OK && clt.OK && fl.OK
	fmt.Println()
	if ready {
		fmt.Println("✅ Your Mac is ready to build iOS apps.")
	} else {
		fmt.Println("⚠️  Some tools are missing — see the notes above before running builds.")
	}
	return ready
}

// CheckXcode verifies a full Xcode is selected (xcodebuild works).
func CheckXcode() Result {
	r := Result{Name: "Xcode"}
	out, err := exec.Command("xcodebuild", "-version").CombinedOutput()
	if err != nil {
		r.Detail = "Not found. Install Xcode from the App Store, then run:\n" +
			"      sudo xcode-select -s /Applications/Xcode.app/Contents/Developer\n" +
			"      sudo xcodebuild -runFirstLaunch"
		return r
	}
	r.OK = true
	r.Detail = firstLine(string(out))
	return r
}

// CheckCommandLineTools verifies the CLT are installed (xcode-select -p resolves).
func CheckCommandLineTools() Result {
	r := Result{Name: "Command Line Tools"}
	out, err := exec.Command("xcode-select", "-p").Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		r.Detail = "Not found. Install with:  xcode-select --install"
		return r
	}
	r.OK = true
	r.Detail = strings.TrimSpace(string(out))
	return r
}

// CheckFastlane verifies Fastlane is on PATH (directly or via Bundler).
func CheckFastlane() Result {
	r := Result{Name: "Fastlane"}
	if path, err := exec.LookPath("fastlane"); err == nil {
		r.OK = true
		r.Detail = path
		return r
	}
	r.Fixable = brewAvailable()
	if r.Fixable {
		r.Detail = "Not found. Can be installed with:  brew install fastlane"
	} else {
		r.Detail = "Not found, and Homebrew isn't installed.\n" +
			"      Install Homebrew from https://brew.sh, then:  brew install fastlane"
	}
	return r
}

// InstallFastlane runs `brew install fastlane`, streaming output to the terminal.
func InstallFastlane() error {
	if !brewAvailable() {
		return fmt.Errorf("Homebrew is required — install it from https://brew.sh first")
	}
	fmt.Println("Installing Fastlane (this can take a few minutes)…")
	cmd := exec.Command("brew", "install", "fastlane")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func brewAvailable() bool {
	_, err := exec.LookPath("brew")
	return err == nil
}

func report(r Result) {
	mark := "✗"
	if r.OK {
		mark = "✓"
	}
	fmt.Printf("  %s %-20s %s\n", mark, r.Name, r.Detail)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func prompt(q string) bool {
	fmt.Print(q)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "" || line == "y" || line == "yes"
}
