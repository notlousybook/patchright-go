//go:build checkout

// Checkout-backed regression test: applies every registered patch to a real
// microsoft/playwright checkout and asserts the key stealth markers land.
// Run with: go test -tags checkout ./patcher/ (needs tmp-playwright at v1.60.0)
package patcher

import (
	"os"
	"strings"
	"testing"
)

func openCheckout(t *testing.T) *FileSet {
	t.Helper()
	dir := os.Getenv("PATCHRIGHT_CHECKOUT")
	if dir == "" {
		dir = "../tmp-playwright"
	}
	fs, err := Open(dir)
	if err != nil {
		t.Skipf("no checkout: %v", err)
	}
	return fs
}

func TestApplyAllOnCheckout(t *testing.T) {
	fs := openCheckout(t)
	if err := ApplyAll(fs); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	if len(fs.Dirty()) == 0 {
		t.Fatal("no files modified")
	}
}

func TestCheckoutStealthMarkers(t *testing.T) {
	fs := openCheckout(t)
	if err := ApplyAll(fs); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	get := func(rel string) string {
		text, err := fs.Get(rel)
		if err != nil {
			t.Fatalf("missing %s: %v", rel, err)
		}
		return text
	}
	// Switch policy.
	switches := get("packages/playwright-core/src/server/chromium/chromiumSwitches.ts")
	if strings.Contains(switches, "--enable-automation") || strings.Contains(switches, "--disable-popup-blocking") {
		t.Error("automation switches not removed")
	}
	if !strings.Contains(switches, "--disable-blink-features=AutomationControlled") {
		t.Error("stealth switch not added")
	}
	// Headless-new + swiftshader.
	chromium := get("packages/playwright-core/src/server/chromium/chromium.ts")
	if !strings.Contains(chromium, "--headless=new") {
		t.Error("--headless=new not forced")
	}
	if strings.Contains(chromium, "--enable-unsafe-swiftshader") {
		t.Error("swiftshader flag not removed")
	}
	// Runtime.enable avoidance.
	for _, rel := range []string{
		"packages/playwright-core/src/server/chromium/crDevTools.ts",
		"packages/playwright-core/src/server/chromium/crServiceWorker.ts",
	} {
		if strings.Contains(get(rel), "send('Runtime.enable')") {
			t.Errorf("%s still enables Runtime", rel)
		}
	}
	// Route injection surface.
	crNet := get("packages/playwright-core/src/server/chromium/crNetworkManager.ts")
	for _, marker := range []string{"_fixCSP", "_injectIntoHead", "patchrightInitScript", "initScriptTag"} {
		if !strings.Contains(crNet, marker) {
			t.Errorf("crNetworkManager missing %q", marker)
		}
	}
	// Protocol additions.
	frameYML := get("packages/protocol/spec/frame.yml")
	if !strings.Contains(frameYML, "isolatedContext") {
		t.Error("frame.yml missing isolatedContext")
	}
	networkYML := get("packages/protocol/spec/network.yml")
	if !strings.Contains(networkYML, "patchrightInitScript") {
		t.Error("network.yml missing patchrightInitScript")
	}
	// Client route installation.
	for _, rel := range []string{
		"packages/playwright-core/src/client/browserContext.ts",
		"packages/playwright-core/src/client/page.ts",
	} {
		if !strings.Contains(get(rel), "installInjectRoute") {
			t.Errorf("%s missing installInjectRoute", rel)
		}
	}
	// Types declarations.
	types := get("packages/playwright-core/types/types.d.ts")
	if !strings.Contains(types, "isolatedContext?: boolean") {
		t.Error("types.d.ts missing isolatedContext")
	}
	// Rebrand.
	program := get("packages/playwright-core/src/cli/program.ts")
	if strings.Contains(program, "ensure browsers necessary for this version of Playwright are installed") {
		t.Error("CLI rebrand incomplete")
	}
	if !strings.Contains(program, "ensure browsers necessary for this version of Patchright are installed") {
		t.Error("CLI rebrand missing")
	}
	if strings.Contains(program, "prints list of browsers from all playwright installations") {
		t.Error("CLI rebrand incomplete (list)")
	}
}
