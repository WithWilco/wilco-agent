package agent

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Every value from a build job is validated against a strict allowlist before it
// reaches a subprocess. Commands are always run as argv (never through a shell),
// so even though jobs arrive over TLS from our own server, a malformed or
// spoofed payload can't inject commands onto this machine. Mirrors the checks in
// the original Python agent.

var (
	branchRe   = regexp.MustCompile(`^[A-Za-z0-9._\-/]{1,255}$`)
	laneRe     = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,64}$`)
	repoPathRe = regexp.MustCompile(`^[A-Za-z0-9._\-/]{1,200}$`)
	sshRepoRe  = regexp.MustCompile(`^git@[A-Za-z0-9.\-]+:[A-Za-z0-9._\-/]+(\.git)?$`)
	// Xcode scheme names allow spaces but nothing that could carry shell tricks.
	// Commands are run as argv (no shell), so spaces are inert; this is defence
	// in depth against a malformed/spoofed scheme value.
	schemeRe = regexp.MustCompile(`^[A-Za-z0-9 ._\-]{1,128}$`)
)

func validateBranch(branch string) (string, error) {
	if branch == "" || !branchRe.MatchString(branch) || strings.Contains(branch, "..") || strings.HasPrefix(branch, "-") {
		return "", fmt.Errorf("rejected unsafe branch name: %q", branch)
	}
	return branch, nil
}

func validateLane(lane string) (string, error) {
	if !laneRe.MatchString(lane) {
		return "", fmt.Errorf("rejected unsafe fastlane lane: %q", lane)
	}
	return lane, nil
}

// validateScheme accepts a non-empty Xcode scheme name from the allowlist, or
// "" (no scheme selected). Leading "-" is rejected so it can't pose as a flag.
func validateScheme(scheme string) (string, error) {
	if scheme == "" {
		return "", nil
	}
	if strings.HasPrefix(scheme, "-") || !schemeRe.MatchString(scheme) {
		return "", fmt.Errorf("rejected unsafe scheme name: %q", scheme)
	}
	return scheme, nil
}

// validateRepoURL accepts only https://host/path(.git) or git@host:path(.git)
// with a safe charset. Rejects anything that could carry shell metacharacters or
// a non-git scheme (ext::, file://, -oProxyCommand tricks, …).
func validateRepoURL(repoURL string) (string, error) {
	if repoURL == "" || strings.HasPrefix(repoURL, "-") {
		return "", fmt.Errorf("rejected unsafe repo URL: %q", repoURL)
	}
	if strings.HasPrefix(repoURL, "git@") {
		if !sshRepoRe.MatchString(repoURL) {
			return "", fmt.Errorf("rejected unsafe SSH repo URL: %q", repoURL)
		}
		return repoURL, nil
	}
	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("rejected unsafe repo URL: %q", repoURL)
	}
	if !repoPathRe.MatchString(strings.TrimPrefix(parsed.Path, "/")) {
		return "", fmt.Errorf("rejected unsafe repo URL: %q", repoURL)
	}
	return repoURL, nil
}
