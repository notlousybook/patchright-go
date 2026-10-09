package browsers

import (
	"strings"
	"testing"
)

func TestSupportedList(t *testing.T) {
	if len(Supported) != 7 {
		t.Errorf("want 7 browsers, got %d", len(Supported))
	}
	seen := map[string]bool{}
	for _, b := range Supported {
		if b.ID == "" || len(b.Names) == 0 {
			t.Errorf("incomplete browser: %+v", b)
		}
		if seen[b.ID] {
			t.Errorf("dup id %q", b.ID)
		}
		seen[b.ID] = true
	}
}

func TestMatchBrowserByPath(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`:        "chrome",
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`:       "edge",
		`/usr/bin/brave-browser-stable`:                                "brave",
		`/Applications/Google Chrome.app/Contents/MacOS/Google Chrome`: "chrome",
		`/opt/opera/opera`: "opera",
	}
	for path, want := range cases {
		b, ok := matchBrowserByPath(path)
		if !ok {
			t.Errorf("no match for %q", path)
			continue
		}
		if b.ID != want {
			t.Errorf("%q matched %q, want %q", path, b.ID, want)
		}
	}
	if _, ok := matchBrowserByPath(`/usr/bin/firefox`); ok {
		t.Error("firefox should not match (not chromium, not supported)")
	}
}

func TestMatchBrowserByName(t *testing.T) {
	b, ok := matchBrowserByName("Google Chrome.lnk")
	if !ok || b.ID != "chrome" {
		t.Errorf("shortcut name match failed: %+v %v", b, ok)
	}
}

func TestLooksLikeExePath(t *testing.T) {
	if !looksLikeExePath(`C:\Program Files\Google\Chrome\Application\chrome.exe`) {
		t.Error("real path rejected")
	}
	if looksLikeExePath(`C:\Program Files\foo\bar.dll`) {
		t.Error("dll accepted")
	}
	if looksLikeExePath("short") {
		t.Error("short string accepted")
	}
}

func TestResolveDesktop(t *testing.T) {
	// unknown file -> empty, must not explode.
	if got := resolveDesktop("definitely-not-here-12345.desktop"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestUsageScoreOrdering(t *testing.T) {
	user := usageScore(`C:\Users\bob\AppData\Local\Google\Chrome\Application\chrome.exe`, "chrome", "install-dir")
	bundle := usageScore(`C:\Users\bob\AppData\Local\ms-playwright\chromium-1234\chrome-win\chrome.exe`, "chromium", "install-dir")
	if user <= bundle {
		t.Errorf("user install (%d) should beat bundle (%d)", user, bundle)
	}
	headless := usageScore(`/root/.cache/ms-playwright/chromium-1/chrome-linux/headless_shell`, "chromium", "install-dir")
	if headless >= bundle {
		t.Errorf("headless shell (%d) should rank last, bundle %d", headless, bundle)
	}
}

func TestDedupeKey(t *testing.T) {
	if dedupeKey(`C:\Foo\BAR`) != dedupeKey(`c:\foo\bar`) {
		t.Error("dedupe should be case-insensitive")
	}
}

func TestDiscoverDoesNotExplode(t *testing.T) {
	// machine-dependent results, but it must return without hanging or panicking.
	found := Discover()
	for _, f := range found {
		if f.Path == "" || f.Browser.ID == "" {
			t.Errorf("incomplete find: %+v", f)
		}
		if strings.Contains(strings.ToLower(f.Browser.ID), "firefox") {
			t.Errorf("firefox snuck in: %+v", f)
		}
	}
	t.Logf("found %d browsers on this machine", len(found))
}
