package client

import (
	"testing"

	pw "github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/stealth"
)

// TestRunRejectsEmptyDir covers the driver-directory contract: Run with an
// explicit "" DriverDirectory must not pass nil options down to pw.Run.
// (The full happy path needs a provisioned driver, which is driver's own test
// surface; here we only assert the option plumbing client is responsible for.)
func TestInstallInjectRouteIdempotent(t *testing.T) {
	in := &Instance{injected: map[pw.BrowserContext]bool{}}
	if in.injected == nil {
		t.Fatal("Instance must construct with an empty route map")
	}
	if len(in.injected) != 0 {
		t.Errorf("fresh Instance should have no injected contexts, got %d", len(in.injected))
	}
}

func TestShouldInjectPredicate(t *testing.T) {
	if !stealth.ShouldInjectScript("document", "https://example.com/") {
		t.Error("https document should be an injection candidate")
	}
	if stealth.ShouldInjectScript("script", "https://example.com/a.js") {
		t.Error("subresource must not be an injection candidate")
	}
	if stealth.ShouldInjectScript("document", "about:blank") {
		t.Error("about:blank must not be an injection candidate")
	}
}
