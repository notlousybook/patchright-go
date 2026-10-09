//go:build e2e

// Package e2e ports utils/detection_smoke_test.mjs to Go. It requires a
// patched Patchright driver plus a Chromium/Chrome binary and is therefore
// gated behind the `e2e` build tag:
//
//	go test -tags e2e ./e2e/
//
// PATCHRIGHT_DRIVER_PATH may point at the patched driver directory; otherwise
// the stock playwright-go driver is used (client-side stealth only).
package e2e

import (
	"os"
	"testing"

	pw "github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/client"
	"github.com/notlousybook/patchright-go/testutil"
)

// evaluateMainWorld mirrors evaluateInMainWorld in the .mjs: it runs the
// script in the main world so isolated-world globals stay invisible.
func evaluateMainWorld(page pw.Page, script string) (any, error) {
	// playwright-go has no isolatedContext knob against a stock driver, so we
	// pass explicit main-world evaluation via a javascript: URL-free helper:
	// evaluate in page context (main world by default on stock drivers).
	return page.Evaluate(script)
}

func detectLeaksScript() string {
	return `(() => {
		const known = ["__pwInitScripts", "__playwright__binding__", "__playwright__binding__controller__"];
		const globals = Object.getOwnPropertyNames(globalThis).filter(
			name => known.includes(name) || /^__(?:pw|playwright)/i.test(name));
		return { webdriver: navigator.webdriver, globals };
	})()`
}

func assertUndetected(t *testing.T, page pw.Page, label string) {
	t.Helper()
	raw, err := evaluateMainWorld(page, detectLeaksScript())
	if err != nil {
		t.Fatalf("%s: leak probe failed: %v", label, err)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s: unexpected probe shape %T", label, raw)
	}
	if wd, _ := m["webdriver"].(bool); wd {
		t.Errorf("%s: navigator.webdriver is true", label)
	}
	if globals, _ := m["globals"].([]any); len(globals) != 0 {
		t.Errorf("%s: Playwright globals leaked: %v", label, globals)
	}
}

func TestDetectionSmoke(t *testing.T) {
	srv, err := testutil.StartTestServer()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	var runOpts []*pw.RunOptions
	if dir := os.Getenv("PATCHRIGHT_DRIVER_PATH"); dir != "" {
		runOpts = append(runOpts, &pw.RunOptions{DriverDirectory: dir})
	}
	inst, err := client.Run(runOpts...)
	if err != nil {
		t.Skipf("driver unavailable: %v", err)
	}
	defer inst.Stop()

	userDataDir := t.TempDir()
	ctx, err := inst.PW.Chromium.LaunchPersistentContext(userDataDir, pw.BrowserTypeLaunchPersistentContextOptions{
		Channel:  pw.String(client.ChromiumChannel),
		Headless: pw.Bool(false),
	})
	if err != nil {
		t.Skipf("chrome channel unavailable: %v", err)
	}
	defer ctx.Close()
	if err := inst.InstallInjectRoute(ctx); err != nil {
		t.Fatal(err)
	}

	pages := ctx.Pages()
	var page pw.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := page.Goto(srv.Origin); err != nil {
		t.Fatal(err)
	}
	assertUndetected(t, page, "initial page")

	if err := page.Locator("#click").Click(); err != nil {
		t.Fatal(err)
	}
	clicks, err := evaluateMainWorld(page, `globalThis.clicks`)
	if err != nil {
		t.Fatal(err)
	}
	if clicks != float64(1) {
		t.Errorf("clicks = %v, want 1", clicks)
	}

	if err := ctx.AddInitScript(pw.Script{Content: ptr("globalThis.userInitScript = true;")}); err != nil {
		t.Fatal(err)
	}
	if err := ctx.ExposeFunction("userBinding", func(args ...any) any { return 42 }); err != nil {
		t.Fatal(err)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatal(err)
	}
	flag, err := evaluateMainWorld(page, `globalThis.userInitScript`)
	if err != nil {
		t.Fatal(err)
	}
	if flag != true {
		t.Errorf("userInitScript = %v, want true", flag)
	}
	bound, err := evaluateMainWorld(page, `globalThis.userBinding()`)
	if err != nil {
		t.Fatal(err)
	}
	if bound != float64(42) {
		t.Errorf("userBinding() = %v, want 42", bound)
	}
	assertUndetected(t, page, "automated page")
}

func ptr(s string) *string { return &s }
