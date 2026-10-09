package browsers

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// lookPath is exec.LookPath without testinterference.
func lookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// searchPATH resolves browser binaries visible on PATH.
func searchPATH() []Found {
	var out []Found
	for _, b := range Supported {
		for _, exe := range candidatesFor(b.ID) {
			base := exe
			// mac entries are subpaths; skip PATH search for those.
			if strings.Contains(base, "/") {
				continue
			}
			if p, err := lookPath(base); err == nil {
				if abs, err := filepath.Abs(p); err == nil {
					p = abs
				}
				out = append(out, Found{Browser: b, Path: p, Source: "path", Uses: usageScore(p, b.ID, "path")})
			}
		}
		// flatpak ids (linux).
		if runtime.GOOS == "linux" {
			for _, id := range flatpakIDs(b.ID) {
				p := filepath.Join("/var/lib/flatpak/exports/bin", id)
				if fileExists(p) {
					out = append(out, Found{Browser: b, Path: p, Source: "path", Uses: usageScore(p, b.ID, "path")})
					continue
				}
				p = filepath.Join(os.Getenv("HOME"), ".local/share/flatpak/exports/bin", id)
				if fileExists(p) {
					out = append(out, Found{Browser: b, Path: p, Source: "path", Uses: usageScore(p, b.ID, "path")})
				}
			}
		}
	}
	return out
}

func flatpakIDs(id string) []string {
	switch id {
	case "chrome":
		return []string{"com.google.Chrome"}
	case "edge":
		return []string{"com.microsoft.Edge"}
	case "brave":
		return []string{"com.brave.Browser"}
	case "opera":
		return []string{"com.opera.Opera"}
	case "vivaldi":
		return []string{"com.vivaldi.Vivaldi"}
	case "chromium":
		return []string{"org.chromium.Chromium"}
	}
	return nil
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+[\d.]*`)

// probeVersion asks the binary for --version. Embedded double-quotes in the
// path are handled by exec directly (no shell). Times out fast so a hanging
// shim can't stall discovery.
func probeVersion(path string) string {
	if path == "" {
		return ""
	}
	done := make(chan string, 1)
	go func() {
		out, err := exec.Command(path, "--version").Output()
		if err != nil {
			done <- ""
			return
		}
		done <- versionRe.FindString(string(out))
	}()
	select {
	case v := <-done:
		return v
	case <-time.After(8 * time.Second):
		return ""
	}
}

// usageScore ranks how likely a browser is the user's main one. user-level
// installs beat system ones, real branded browsers beat raw chromium and
// playwright bundles, newer versions win ties (handled by sort, but a small
// bump here keeps bundle copies last).
func usageScore(path, id, source string) int {
	score := 0
	switch source {
	case "install-dir":
		score += 30
	case "shortcut":
		score += 20
	case "registry":
		score += 25
	case "path":
		score += 10
	}
	lower := strings.ToLower(path)
	// user profile installs = daily driver material.
	if strings.Contains(lower, "appdata\\local") || strings.Contains(lower, "/home/") || strings.Contains(lower, "/users/") {
		if !strings.Contains(lower, "ms-playwright") && !strings.Contains(lower, ".cache") {
			score += 20
		}
	}
	// playwright's own bundle is a fallback, not the main browser.
	if strings.Contains(lower, "ms-playwright") {
		score -= 15
	}
	// headless shells are never the main browser.
	if strings.Contains(lower, "headless") {
		score -= 50
	}
	// brand preference: chrome > edge > brave > opera > vivaldi > arc > chromium.
	score += (len(Supported) - browserOrder(id)) * 2
	return score
}
