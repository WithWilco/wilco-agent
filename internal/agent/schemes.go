package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SchemeRequest asks the agent to enumerate a repo's Xcode schemes so the user
// can pick which one to build. It clones the repo and runs `xcodebuild -list`.
type SchemeRequest struct {
	RequestID string `json:"request_id"`
	RepoURL   string `json:"repo_url"`
	Branch    string `json:"branch"`
}

// discoverSchemes shallow-clones the repo and returns its Xcode scheme names.
// The clone is always removed before returning. Any failure yields an empty
// list (callers surface "no schemes found" rather than a hard error).
func discoverSchemes(ctx context.Context, req SchemeRequest, workDirRaw string) ([]string, error) {
	repoURL, err := validateRepoURL(req.RepoURL)
	if err != nil {
		return nil, err
	}
	branch, err := validateBranch(req.Branch)
	if err != nil {
		return nil, err
	}

	workDir := expandHome(workDirRaw)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create work dir: %w", err)
	}

	repoName := strings.TrimSuffix(filepath.Base(repoURL), ".git")
	cloneDir := filepath.Join(workDir, fmt.Sprintf("%s-schemes", repoName))
	_ = os.RemoveAll(cloneDir)
	defer os.RemoveAll(cloneDir)

	// Shallow clone is enough — we only inspect project structure, not history.
	if code := runSilent(ctx, workDir,
		"git", "clone", "--depth", "1", "--branch", branch, "--", repoURL, cloneDir); code != 0 {
		return nil, fmt.Errorf("clone failed for %s @ %s", repoURL, branch)
	}

	return listSchemes(ctx, cloneDir)
}

// listSchemes runs `xcodebuild -list -json` against the workspace or project
// found in dir and parses out the scheme names.
func listSchemes(ctx context.Context, dir string) ([]string, error) {
	args := []string{"-list", "-json"}
	if ws := findContainer(dir, ".xcworkspace"); ws != "" {
		args = append(args, "-workspace", ws)
	} else if proj := findContainer(dir, ".xcodeproj"); proj != "" {
		args = append(args, "-project", proj)
	}

	cmd := exec.CommandContext(ctx, "xcodebuild", args...)
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("xcodebuild -list failed: %w", err)
	}

	var parsed struct {
		Project   struct{ Schemes []string } `json:"project"`
		Workspace struct{ Schemes []string } `json:"workspace"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("could not parse xcodebuild output: %w", err)
	}
	schemes := parsed.Workspace.Schemes
	if len(schemes) == 0 {
		schemes = parsed.Project.Schemes
	}
	return schemes, nil
}

// findContainer returns the path (relative to dir) of the first *.suffix bundle
// in the repo, skipping dependency dirs and the project.xcworkspace nested
// inside an .xcodeproj. Returns "" when none is found.
func findContainer(dir, suffix string) string {
	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "Pods" || base == "node_modules" || base == "Carthage" {
				return filepath.SkipDir
			}
			if strings.HasSuffix(base, suffix) {
				// Ignore the implicit workspace Xcode puts inside every project.
				if suffix == ".xcworkspace" && strings.HasSuffix(filepath.Dir(path), ".xcodeproj") {
					return filepath.SkipDir
				}
				if rel, e := filepath.Rel(dir, path); e == nil {
					found = rel
				}
				return filepath.SkipDir
			}
		}
		return nil
	})
	return found
}

// runSilent runs a command discarding its output, returning the exit code.
func runSilent(ctx context.Context, cwd string, argv ...string) int {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if isDir(cwd) {
		cmd.Dir = cwd
	}
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}
