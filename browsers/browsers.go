// Package browsers finds real chromium browsers installed on the machine.
//
// playwright's channel launch ("chrome", "msedge", ...) only works when the
// driver knows where stuff lives, and half the time it doesn't, especially on
// weird setups. so this package looks EVERYWHERE: normal install locations on
// windows/mac/linux, start menu shortcuts, desktop shortcuts, registry
// App Paths, and PATH. then it picks the most used one (by launch count
// heuristics: user-level install beats system, newer version beats older) and
// hands you an executable path you can feed to playwright.
//
// only chromium-based browsers. firefox/webkit need not apply, same as the
// rest of this repo.
package browsers

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Browser is one supported chromium-based browser.
type Browser struct {
	// ID is the short name: chrome, edge, brave, opera, vivaldi, chromium, arc.
	ID string
	// Names are the human names to match in shortcuts and file names.
	Names []string
	// Channel is the playwright channel name, empty when there's no channel
	// and you have to launch by executable path.
	Channel string
}

// Supported lists every browser we know how to find, most popular first.
var Supported = []Browser{
	{ID: "chrome", Names: []string{"Google Chrome", "Chrome"}, Channel: "chrome"},
	{ID: "edge", Names: []string{"Microsoft Edge", "Edge"}, Channel: "msedge"},
	{ID: "brave", Names: []string{"Brave"}, Channel: "brave"},
	{ID: "opera", Names: []string{"Opera"}, Channel: "opera"},
	{ID: "vivaldi", Names: []string{"Vivaldi"}, Channel: ""},
	{ID: "arc", Names: []string{"Arc"}, Channel: ""},
	{ID: "chromium", Names: []string{"Chromium"}, Channel: "chromium"},
}

// Found is one discovered browser installation.
type Found struct {
	Browser Browser
	// Path is the browser executable (or launcher shim).
	Path string
	// Version is the probed version string, may be empty if probing failed.
	Version string
	// Source says where we found it: "install-dir", "path", "shortcut",
	// "registry", "applications".
	Source string
	// Uses is the usage score. higher = more used = picked first.
	Uses int
}

// candidatesFor returns per-OS executable file names for a browser id.
func candidatesFor(id string) []string {
	switch runtime.GOOS {
	case "windows":
		switch id {
		case "chrome":
			return []string{"chrome.exe"}
		case "edge":
			return []string{"msedge.exe"}
		case "brave":
			return []string{"brave.exe"}
		case "opera":
			return []string{"opera.exe"}
		case "vivaldi":
			return []string{"vivaldi.exe"}
		case "arc":
			return []string{"Arc.exe"}
		case "chromium":
			return []string{"chrome.exe", "chromium.exe"}
		}
	case "darwin":
		return []string{"Contents/MacOS/" + macBinary(id)}
	default: // linux and friends
		switch id {
		case "chrome":
			return []string{"google-chrome", "google-chrome-stable", "chrome"}
		case "edge":
			return []string{"microsoft-edge", "microsoft-edge-stable", "msedge"}
		case "brave":
			return []string{"brave-browser", "brave-browser-stable", "brave"}
		case "opera":
			return []string{"opera", "opera-stable"}
		case "vivaldi":
			return []string{"vivaldi", "vivaldi-stable"}
		case "arc":
			return []string{"arc"}
		case "chromium":
			return []string{"chromium", "chromium-browser", "chromium-stable"}
		}
	}
	return nil
}

func macBinary(id string) string {
	switch id {
	case "chrome":
		return "Google Chrome"
	case "edge":
		return "Microsoft Edge"
	case "brave":
		return "Brave Browser"
	case "opera":
		return "Opera"
	case "vivaldi":
		return "Vivaldi"
	case "arc":
		return "Arc"
	case "chromium":
		return "Chromium"
	}
	return id
}

// installDirs returns per-OS directories to search for browser installs.
func installDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pfx86 := os.Getenv("ProgramFiles(x86)")
		local := os.Getenv("LOCALAPPDATA")
		progdata := os.Getenv("ProgramData")
		dirs = []string{
			filepath.Join(local, "Google", "Chrome", "Application"),
			filepath.Join(pf, "Google", "Chrome", "Application"),
			filepath.Join(pfx86, "Google", "Chrome", "Application"),
			filepath.Join(local, "Microsoft", "Edge", "Application"),
			filepath.Join(pf, "Microsoft", "Edge", "Application"),
			filepath.Join(pfx86, "Microsoft", "Edge", "Application"),
			filepath.Join(local, "BraveSoftware", "Brave-Browser", "Application"),
			filepath.Join(pf, "BraveSoftware", "Brave-Browser", "Application"),
			filepath.Join(pfx86, "BraveSoftware", "Brave-Browser", "Application"),
			filepath.Join(local, "Vivaldi", "Application"),
			filepath.Join(pf, "Vivaldi", "Application"),
			filepath.Join(local, "Arc", "Application"),
			filepath.Join(local, "Chromium", "Application"),
			filepath.Join(pf, "Chromium", "Application"),
			filepath.Join(local, "Programs", "Opera"),
			filepath.Join(pf, "Opera"),
			filepath.Join(pfx86, "Opera"),
			// playwright's own bundled browsers (nice fallback)
			filepath.Join(home, "AppData", "Local", "ms-playwright"),
			filepath.Join(progdata, "ms-playwright"),
		}
	case "darwin":
		dirs = []string{
			"/Applications",
			filepath.Join(home, "Applications"),
			// playwright bundled browsers
			filepath.Join(home, "Library", "Caches", "ms-playwright"),
		}
	default:
		dirs = []string{
			"/usr/bin",
			"/usr/local/bin",
			"/opt/google/chrome",
			"/opt/microsoft/msedge",
			"/opt/brave.com/brave",
			"/opt/opera",
			"/opt/vivaldi",
			"/snap/bin",
			"/var/lib/flatpak/exports/bin",
			filepath.Join(home, ".cache", "ms-playwright"),
			"/root/.cache/ms-playwright",
		}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, ".local", "bin"))
		}
	}
	return dirs
}

// shortcutDirs returns per-OS directories holding .lnk / .desktop / app
// shortcuts that might point at browsers.
func shortcutDirs() []string {
	var dirs []string
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		appdata := os.Getenv("APPDATA")
		progdata := os.Getenv("ProgramData")
		dirs = []string{
			filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs"),
			filepath.Join(progdata, "Microsoft", "Windows", "Start Menu", "Programs"),
			filepath.Join(home, "Desktop"),
			filepath.Join(home, "OneDrive", "Desktop"),
			"C:\\Users\\Public\\Desktop",
		}
	case "darwin":
		// .app bundles are the shortcuts on mac; handled via installDirs.
		// also check chrome's own KS admin dir for version hints, skip here.
		dirs = nil
	default:
		dirs = []string{
			"/usr/share/applications",
			"/usr/local/share/applications",
			filepath.Join(home, ".local", "share", "applications"),
			"/var/lib/flatpak/exports/share/applications",
			filepath.Join(home, ".local", "share", "flatpak", "exports", "share", "applications"),
		}
	}
	return dirs
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// dedupeKey normalizes a path for dedup.
func dedupeKey(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return strings.ToLower(filepath.Clean(abs))
}

// Discover searches everywhere and returns all found browsers, best first
// (highest Uses score, then version, then id order in Supported).
func Discover() []Found {
	seen := map[string]bool{}
	var out []Found
	add := func(f Found) {
		key := f.Browser.ID + "\x00" + dedupeKey(f.Path)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, f)
	}
	// 1. install dirs (highest trust).
	for _, dir := range installDirs() {
		for _, b := range Supported {
			for _, exe := range candidatesFor(b.ID) {
				full := filepath.Join(dir, exe)
				// mac .app bundles: dir itself is /Applications, exe has subpath.
				if runtime.GOOS == "darwin" && (dir == "/Applications" || strings.HasSuffix(dir, "Applications")) {
					for _, app := range macAppNames(b.ID) {
						p := filepath.Join(dir, app, exe)
						if fileExists(p) {
							add(Found{Browser: b, Path: p, Source: "install-dir", Uses: usageScore(p, b.ID, "install-dir")})
						}
					}
					continue
				}
				// playwright bundle dirs contain versioned subdirs (chromium-1234/chrome-linux/chrome).
				if strings.Contains(dir, "ms-playwright") {
					for _, p := range searchBundleDir(dir, b) {
						add(Found{Browser: b, Path: p, Source: "install-dir", Uses: usageScore(p, b.ID, "install-dir")})
					}
					continue
				}
				if fileExists(full) {
					add(Found{Browser: b, Path: full, Source: "install-dir", Uses: usageScore(full, b.ID, "install-dir")})
				}
			}
		}
	}
	// 2. shortcuts.
	for _, f := range searchShortcuts() {
		add(f)
	}
	// 3. registry App Paths (windows).
	for _, f := range searchRegistry() {
		add(f)
	}
	// 4. PATH.
	for _, f := range searchPATH() {
		add(f)
	}
	// Probe versions + rank.
	for i := range out {
		out[i].Version = probeVersion(out[i].Path)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Uses != out[j].Uses {
			return out[i].Uses > out[j].Uses
		}
		if out[i].Version != out[j].Version {
			return out[i].Version > out[j].Version
		}
		return browserOrder(out[i].Browser.ID) < browserOrder(out[j].Browser.ID)
	})
	return out
}

// Default returns the most used browser, or nil if nothing found.
func Default() *Found {
	all := Discover()
	if len(all) == 0 {
		return nil
	}
	return &all[0]
}

func browserOrder(id string) int {
	for i, b := range Supported {
		if b.ID == id {
			return i
		}
	}
	return len(Supported)
}

// macAppNames returns .app bundle names for a browser id.
func macAppNames(id string) []string {
	switch id {
	case "chrome":
		return []string{"Google Chrome.app"}
	case "edge":
		return []string{"Microsoft Edge.app"}
	case "brave":
		return []string{"Brave Browser.app"}
	case "opera":
		return []string{"Opera.app"}
	case "vivaldi":
		return []string{"Vivaldi.app"}
	case "arc":
		return []string{"Arc.app"}
	case "chromium":
		return []string{"Chromium.app"}
	}
	return nil
}

// searchBundleDir looks inside playwright's ms-playwright cache for usable
// chromium binaries (chromium-*/chrome-{linux,mac,win}/chrome[.exe]).
func searchBundleDir(dir string, b Browser) []string {
	if b.ID != "chromium" && b.ID != "chrome" {
		return nil
	}
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "chromium-") && !strings.HasPrefix(name, "chrome-") {
			continue
		}
		base := filepath.Join(dir, name)
		for _, p := range []string{
			filepath.Join(base, "chrome-linux", "chrome"),
			filepath.Join(base, "chrome-linux", "headless_shell"),
			filepath.Join(base, "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium"),
			filepath.Join(base, "chrome-win", "chrome.exe"),
		} {
			if fileExists(p) {
				out = append(out, p)
			}
		}
	}
	return out
}
