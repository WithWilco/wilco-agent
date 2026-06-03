package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// expandHome turns a leading ~ into the user's home directory.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// largestArtifact walks the build dir for .ipa/.apk/.aab files and returns the
// size of the biggest one, logging each as it's found.
func largestArtifact(buildDir string, sendLog func(string)) int64 {
	var largest int64
	exts := map[string]bool{".ipa": true, ".apk": true, ".aab": true}
	_ = filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if exts[strings.ToLower(filepath.Ext(path))] {
			size := info.Size()
			if size > largest {
				largest = size
				sendLog(fmt.Sprintf("[agent] Found artifact: %s (%.1f MB)\n",
					filepath.Base(path), float64(size)/1024/1024))
			}
		}
		return nil
	})
	return largest
}

// tail joins the last n lines of a slice with newlines.
func tail(lines []string, n int) string {
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
