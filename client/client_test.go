package client

import (
	"testing"

	"github.com/notlousybook/patchright-go/stealth"
)

func TestStealthLaunchOptions(t *testing.T) {
	opt := StealthLaunchOptions(true, []string{"--enable-automation", "--foo=bar"})
	if opt.Headless == nil || !*opt.Headless {
		t.Error("headless not set")
	}
	joined := ""
	for _, a := range opt.Args {
		joined += a + " "
	}
	if containsStr(joined, "--enable-automation") {
		t.Errorf("automation flag not stripped: %q", joined)
	}
	if !containsStr(joined, "--disable-blink-features=AutomationControlled") {
		t.Errorf("stealth flag missing: %q", joined)
	}
	if !containsStr(joined, "--foo=bar") {
		t.Errorf("user arg dropped: %q", joined)
	}
}

func TestShouldInjectPredicate(t *testing.T) {
	if !stealth.ShouldInjectScript("document", "https://example.com/") {
		t.Error("https document should be an injection candidate")
	}
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
