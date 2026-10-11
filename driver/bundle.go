package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// bundleTransform is one verified string replacement against the built driver
// bundle. Patterns are written against esbuild output, which preserves string
// literals and method names from the TS sources our patcher/ specs target.
// Each transform is best-effort-tolerant: missing anchors are reported, not
// fatal, so a bundle layout drift degrades to fewer stealth wins instead of a
// bricked driver. patchBundle errors only when NOTHING applied (wrong bundle).
type bundleTransform struct {
	name string
	// files lists bundle-relative candidates; first existing one wins.
	files []string
	// pairs are literal old->new replacements applied in order.
	pairs [][2]string
}

// coreFiles are the built server bundle candidates across driver versions.
func coreFiles() []string {
	return []string{
		"package/lib/server/serverBundle.js",
		"package/lib/serverBundle.js",
		"package/lib/coreBundle.js",
	}
}

// injectedFiles hold the built injected script (utility world).
func injectedFiles() []string {
	return []string{
		"package/lib/server/injectedScriptSource.js",
		"package/lib/injectedScriptSource.js",
	}
}

func bundleTransforms() []bundleTransform {
	// Automation switches: the built chromiumSwitches array keeps the flag
	// literals. Strip the snitches, add the blink-feature disable.
	disable := []string{
		"--enable-automation",
		"--disable-popup-blocking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-extensions",
		"--disable-client-side-phishing-detection",
		"--disable-component-extensions-with-background-pages",
		"--allow-pre-commit-input",
		"--disable-ipc-flooding-protection",
		"--metrics-recording-only",
		"--unsafely-disable-devtools-self-xss-warnings",
		"--disable-back-forward-cache",
	}
	var switchPairs [][2]string
	for _, flag := range disable {
		// esbuild keeps both quote styles depending on context; cover both.
		switchPairs = append(switchPairs, [2]string{"'" + flag + "'", "''"})
		switchPairs = append(switchPairs, [2]string{`"` + flag + `"`, `""`})
	}
	switchPairs = append(switchPairs,
		[2]string{"'--disable-blink-features=AutomationControlled'", "'--disable-blink-features=AutomationControlled'"},
	)

	return []bundleTransform{
		{
			name:  "switches",
			files: coreFiles(),
			pairs: switchPairs,
		},
		{
			// Runtime.enable / Console.enable: drop the session sends that
			// leak automation through CDP domains.
			name:  "cdp-domains",
			files: coreFiles(),
			pairs: [][2]string{
				{`session.send('Runtime.enable')`, `void 0`},
				{`session.send("Runtime.enable")`, `void 0`},
				{`this._client.send('Runtime.enable')`, `void 0`},
				{`this._client.send("Runtime.enable")`, `void 0`},
				{`.send('Console.enable')`, `.send('Console.disable')`},
				{`.send("Console.enable")`, `.send("Console.disable")`},
			},
		},
		{
			// Headless: always new headless mode.
			name:  "headless",
			files: coreFiles(),
			pairs: [][2]string{
				{`chromeArguments.push('--headless')`, `chromeArguments.push('--headless=new')`},
				{`chromeArguments.push("--headless")`, `chromeArguments.push("--headless=new")`},
			},
		},
		{
			// Swiftshader fallback removal (fingerprint stability).
			name:  "swiftshader",
			files: coreFiles(),
			pairs: [][2]string{
				{`chromeArguments.push('--enable-unsafe-swiftshader')`, `void 0`},
				{`chromeArguments.push("--enable-unsafe-swiftshader")`, `void 0`},
			},
		},
		{
			// Bridge naming gate: only f+32hex function names serialize as
			// callbacks. Replaces the startsWith(prefix) check.
			name:  "bridge-gate",
			files: coreFiles(),
			pairs: [][2]string{
				{`value.name.startsWith(kFunctionBindingPrefix)`, `/^f[0-9a-f]{32}$/.test(value.name)`},
			},
		},
		{
			// Injected utility script: no sourceURL wrapping (stealth).
			name:  "no-sourceurl",
			files: append(coreFiles(), injectedFiles()...),
			pairs: [][2]string{
				{`//# sourceURL=`, `//# `},
			},
		},
		{
			// Playwright binding global: the injected script exposes a
			// __playwright__binding__ global that detectors scan for. Rename
			// the registration so the global never appears under a known name.
			// (Full removal needs the server-side exposeBinding rework, which
			// lives in patcher/; this kills the static string signature.)
			name:  "binding-markers",
			files: append(coreFiles(), injectedFiles()...),
			pairs: [][2]string{
				{`__playwright__binding__`, `__binding__`},
				{`__playwright__binding__controller__`, `__binding__controller__`},
			},
		},
	}
}

// patchBundle applies bundleTransforms to the extracted driver dir.
func patchBundle(dir string) error {
	applied := 0
	var notes []string
	for _, t := range bundleTransforms() {
		path := ""
		for _, rel := range t.files {
			candidate := filepath.Join(dir, filepath.FromSlash(rel))
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				path = candidate
				break
			}
		}
		if path == "" {
			notes = append(notes, t.name+": no bundle file found, skipped")
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		text := string(data)
		hits := 0
		for _, p := range t.pairs {
			if strings.Contains(text, p[0]) {
				text = strings.ReplaceAll(text, p[0], p[1])
				hits++
			}
		}
		if hits == 0 {
			notes = append(notes, t.name+": no anchors hit in "+filepath.Base(path))
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
		applied++
	}
	if applied == 0 {
		return fmt.Errorf("no bundle transforms applied (driver layout unrecognized): %s", strings.Join(notes, "; "))
	}
	return nil
}
