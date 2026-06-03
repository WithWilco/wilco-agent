package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Job is a build request received from the server.
type Job struct {
	BuildID      int    `json:"build_id"`
	RepoURL      string `json:"repo_url"`
	Branch       string `json:"branch"`
	Platform     string `json:"platform"`
	FastlaneLane string `json:"fastlane_lane"`
	Scheme       string `json:"scheme"`
}

type buildResult struct {
	exitCode      int
	errorSummary  string
	artifactBytes int64
}

// runBuild clones the repo, installs deps, and runs the Fastlane lane, streaming
// every output line through sendLog. It mirrors the original Python agent's flow.
func runBuild(ctx context.Context, job Job, workDirRaw string, cleanup bool, sendLog func(string)) buildResult {
	res := buildResult{exitCode: -1}
	var errorLines []string
	recordErr := func(line string) {
		l := strings.ToLower(line)
		if strings.Contains(l, "error") || strings.Contains(l, "fatal") || strings.Contains(l, "failed") {
			errorLines = append(errorLines, strings.TrimRight(line, "\n"))
			if len(errorLines) > 50 {
				errorLines = errorLines[len(errorLines)-50:]
			}
		}
	}
	stream := func(line string) {
		sendLog(line)
		recordErr(line)
	}

	// Validate the job before anything touches a subprocess.
	repoURL, err := validateRepoURL(job.RepoURL)
	if err != nil {
		sendLog(fmt.Sprintf("\n[agent] ERROR: %v\n", err))
		res.exitCode = 1
		return finalize(res, errorLines, "", cleanup, sendLog)
	}
	branch, err := validateBranch(job.Branch)
	if err != nil {
		sendLog(fmt.Sprintf("\n[agent] ERROR: %v\n", err))
		res.exitCode = 1
		return finalize(res, errorLines, "", cleanup, sendLog)
	}
	lane := job.FastlaneLane
	if lane == "" {
		lane = "beta"
	}
	lane, err = validateLane(lane)
	if err != nil {
		sendLog(fmt.Sprintf("\n[agent] ERROR: %v\n", err))
		res.exitCode = 1
		return finalize(res, errorLines, "", cleanup, sendLog)
	}
	scheme, err := validateScheme(job.Scheme)
	if err != nil {
		sendLog(fmt.Sprintf("\n[agent] ERROR: %v\n", err))
		res.exitCode = 1
		return finalize(res, errorLines, "", cleanup, sendLog)
	}

	workDir := expandHome(workDirRaw)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		sendLog(fmt.Sprintf("\n[agent] ERROR: cannot create work dir: %v\n", err))
		res.exitCode = 1
		return finalize(res, errorLines, "", cleanup, sendLog)
	}

	repoName := strings.TrimSuffix(filepath.Base(repoURL), ".git")
	buildDir := filepath.Join(workDir, fmt.Sprintf("%s-build-%d", repoName, job.BuildID))

	host, _ := os.Hostname()
	sendLog(fmt.Sprintf("[agent] Build #%d started on %s\n", job.BuildID, host))
	sendLog(fmt.Sprintf("[agent] Platform: %s | Lane: %s\n", job.Platform, lane))
	sendLog(fmt.Sprintf("[agent] Repo: %s @ %s\n", repoURL, branch))
	sendLog(fmt.Sprintf("[agent] %s\n", strings.Repeat("─", 60)))

	_ = os.RemoveAll(buildDir)

	// Clone (shallow first, then full as a fallback).
	sendLog("[agent] Cloning repository...\n")
	code := runArgv(ctx, workDir, nil, stream,
		"git", "clone", "--depth", "1", "--branch", branch, "--", repoURL, buildDir)
	if code != 0 {
		sendLog("[agent] Shallow clone failed, trying full clone...\n")
		_ = os.RemoveAll(buildDir)
		code = runArgv(ctx, workDir, nil, stream, "git", "clone", "--", repoURL, buildDir)
		if code == 0 {
			code = runArgv(ctx, buildDir, nil, stream, "git", "checkout", branch)
		}
		if code != 0 {
			sendLog("\n[agent] ERROR: Failed to clone repository\n")
			res.exitCode = code
			return finalize(res, errorLines, buildDir, cleanup, sendLog)
		}
	}

	// Install dependencies.
	sendLog("\n[agent] Installing dependencies...\n")
	hasGemfile := fileExists(filepath.Join(buildDir, "Gemfile"))
	if hasGemfile {
		runArgv(ctx, buildDir, nil, stream, "bundle", "install", "--jobs", "4", "--retry", "3")
	}
	if job.Platform == "ios" && fileExists(filepath.Join(buildDir, "Podfile")) {
		sendLog("[agent] Running pod install...\n")
		runArgv(ctx, buildDir, nil, stream, "bundle", "exec", "pod", "install", "--repo-update")
	}

	// Run Fastlane.
	sendLog(fmt.Sprintf("\n[agent] %s\n", strings.Repeat("─", 60)))
	if scheme != "" {
		sendLog(fmt.Sprintf("[agent] Running: fastlane %s scheme:%s\n", lane, scheme))
	} else {
		sendLog(fmt.Sprintf("[agent] Running: fastlane %s\n", lane))
	}
	sendLog(fmt.Sprintf("[agent] %s\n\n", strings.Repeat("─", 60)))

	flEnv := []string{
		"FASTLANE_HIDE_CHANGELOG=1",
		"FASTLANE_SKIP_UPDATE_CHECK=1",
		"FASTLANE_DISABLE_COLORS=1",
		"CI=1",
	}
	// Expose the scheme both as a Fastfile parameter (options[:scheme]) and an
	// env var (ENV["WILCO_SCHEME"]) so either style of Fastfile can pick it up.
	var fastlaneArgv []string
	if hasGemfile {
		fastlaneArgv = []string{"bundle", "exec", "fastlane", lane}
	} else {
		fastlaneArgv = []string{"fastlane", lane}
	}
	if scheme != "" {
		flEnv = append(flEnv, "WILCO_SCHEME="+scheme)
		fastlaneArgv = append(fastlaneArgv, "scheme:"+scheme)
	}
	res.exitCode = runArgv(ctx, buildDir, flEnv, stream, fastlaneArgv...)

	return finalize(res, errorLines, buildDir, cleanup, sendLog)
}

func finalize(res buildResult, errorLines []string, buildDir string, cleanup bool, sendLog func(string)) buildResult {
	if buildDir != "" {
		res.artifactBytes = largestArtifact(buildDir, sendLog)
	}
	sendLog(fmt.Sprintf("\n[agent] %s\n", strings.Repeat("─", 60)))
	if res.exitCode == 0 {
		sendLog("[agent] Build PASSED ✓\n")
	} else {
		sendLog(fmt.Sprintf("[agent] Build FAILED ✗ (exit code %d)\n", res.exitCode))
	}
	res.errorSummary = tail(errorLines, 10)

	if cleanup && buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	return res
}

// runArgv runs a command from an argv slice (NO shell), streaming combined
// stdout/stderr line by line. Returns the process exit code. Passing values as
// argv means shell metacharacters in a branch or URL are inert.
func runArgv(ctx context.Context, cwd string, envExtra []string, sendLine func(string), argv ...string) int {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if isDir(cwd) {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), envExtra...)

	// One pipe carries both stdout and stderr so logs stay interleaved in order.
	pr, pw, err := os.Pipe()
	if err != nil {
		sendLine(fmt.Sprintf("[agent] ERROR: %v\n", err))
		return 1
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		_ = pr.Close()
		sendLine(fmt.Sprintf("[agent] ERROR: could not start %s: %v\n", argv[0], err))
		return 1
	}
	// Close the parent's write end so the reader sees EOF when the child exits.
	_ = pw.Close()

	reader := bufio.NewReader(pr)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			sendLine(line)
		}
		if readErr != nil {
			break
		}
	}
	_ = pr.Close()

	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
