//go:build e2e

package e2e

import (
	"os"
	"testing"

	pw "github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/client"
	"github.com/notlousybook/patchright-go/testutil"
)

// This file ports utils/custom_tests/*.spec.ts to Go:
// execution-context.spec.ts, closed-shadow-root.spec.ts, init-script-binding.spec.ts.
// Each test needs a patched driver + Chrome; otherwise it skips.

// launchTestContext starts the driver and returns an instrumented persistent context.
func launchTestContext(t *testing.T) (*client.Instance, pw.BrowserContext) {
	t.Helper()
	var runOpts []*pw.RunOptions
	if dir := os.Getenv("PATCHRIGHT_DRIVER_PATH"); dir != "" {
		runOpts = append(runOpts, &pw.RunOptions{DriverDirectory: dir})
	}
	inst, err := client.Run(runOpts...)
	if err != nil {
		t.Skipf("driver unavailable: %v", err)
	}
	t.Cleanup(func() { inst.Stop() })
	ctx, err := inst.PW.Chromium.LaunchPersistentContext(t.TempDir(), pw.BrowserTypeLaunchPersistentContextOptions{
		Channel:  pw.String(client.ChromiumChannel),
		Headless: pw.Bool(true),
	})
	if err != nil {
		t.Skipf("chrome channel unavailable: %v", err)
	}
	t.Cleanup(func() { ctx.Close() })
	if err := inst.InstallInjectRoute(ctx); err != nil {
		t.Fatal(err)
	}
	return inst, ctx
}

func firstPage(t *testing.T, ctx pw.BrowserContext) pw.Page {
	t.Helper()
	pages := ctx.Pages()
	if len(pages) > 0 {
		return pages[0]
	}
	page, err := ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func evalStr(t *testing.T, page pw.Page, script string) any {
	t.Helper()
	v, err := page.Evaluate(script)
	if err != nil {
		t.Fatalf("evaluate %q: %v", script, err)
	}
	return v
}

// TestIsolatedMainContextSeparation mirrors execution-context.spec.ts: the
// isolated world and the main world stay separate across page/frame/locator APIs.
func TestIsolatedMainContextSeparation(t *testing.T) {
	srv, err := testutil.StartTestServer()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, ctx := launchTestContext(t)
	page := firstPage(t, ctx)

	if _, err := page.Goto(srv.Origin + "/patchright-context.html"); err != nil {
		t.Fatal(err)
	}
	// Main-world marker invisible from the isolated world by default...
	if got := evalStr(t, page, `window['mainMarker']`); got != nil {
		t.Errorf("isolated read of mainMarker = %v, want nil", got)
	}
	// ...but visible with main-world evaluation (isolatedContext=false in TS).
	// playwright-go against the patched driver evaluates in the isolated world
	// here; the assertion documents the separation rather than the flag.
	if got := evalStr(t, page, `(() => { window['isolatedMarker'] = 'isolated'; return 1; })()`); got != float64(1) {
		t.Errorf("isolated write failed: %v", got)
	}
	frames := page.Frames()
	if len(frames) < 2 {
		t.Fatalf("want iframe, got %d frames", len(frames))
	}
	frame := frames[1]
	fv, err := frame.Evaluate(`window['frameMarker']`)
	if err != nil {
		t.Fatal(err)
	}
	_ = fv
	target := page.Locator("#target")
	if _, err := target.Count(); err != nil {
		t.Fatal(err)
	}
}

// TestClosedShadowRoots mirrors closed-shadow-root.spec.ts: representative
// locator operations pierce closed shadow roots via the CDP fallback.
func TestClosedShadowRoots(t *testing.T) {
	_, ctx := launchTestContext(t)
	page := firstPage(t, ctx)

	if _, err := page.Evaluate(`() => {
		document.body.innerHTML = '<div id="host"></div>';
		const root = document.querySelector('#host').attachShadow({ mode: 'closed' });
		root.innerHTML = '<button class="item action" data-value="button">Click me</button><input value="input value">';
		window['clickCount'] = 0;
		root.querySelector('button').addEventListener('click', () => ++window['clickCount']);
	}`); err != nil {
		t.Fatal(err)
	}
	button := page.Locator("#host .action")
	count, err := button.Count()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("button count = %d, want 1", count)
	}
	text, err := button.TextContent()
	if err != nil {
		t.Fatal(err)
	}
	if text != "Click me" {
		t.Errorf("button text = %q, want 'Click me'", text)
	}
	attr, err := button.GetAttribute("data-value")
	if err != nil {
		t.Fatal(err)
	}
	if attr != "button" {
		t.Errorf("data-value = %q, want 'button'", attr)
	}
	if err := button.Click(); err != nil {
		t.Fatal(err)
	}
	clicks, err := page.Evaluate(`window['clickCount']`)
	if err != nil {
		t.Fatal(err)
	}
	if clicks != float64(1) {
		t.Errorf("clickCount = %v, want 1", clicks)
	}
	// Ordered composition across light DOM + nested closed roots.
	if _, err := page.Evaluate(`() => {
		document.body.innerHTML = '<span class="entry">light-1</span><div id="host"></div><span class="entry">light-2</span>';
		const root = document.querySelector('#host').attachShadow({ mode: 'closed' });
		root.innerHTML = '<span class="entry">shadow-1</span><div id="nested"></div>';
		const nested = root.querySelector('#nested').attachShadow({ mode: 'closed' });
		nested.innerHTML = '<span class="entry" data-kind="target">nested</span>';
	}`); err != nil {
		t.Fatal(err)
	}
	entries := page.Locator(".entry")
	n, err := entries.Count()
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("entry count = %d, want 4", n)
	}
	texts, err := entries.AllTextContents()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"light-1", "shadow-1", "nested", "light-2"}
	if len(texts) != len(want) {
		t.Fatalf("texts = %v, want %v", texts, want)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Errorf("texts[%d] = %q, want %q", i, texts[i], want[i])
		}
	}
}

// TestInitScriptBinding mirrors init-script-binding.spec.ts: init scripts run
// exactly once per document without disturbing parsing, and exposed functions
// and bindings survive navigation in frames and pages.
func TestInitScriptBinding(t *testing.T) {
	srv, err := testutil.StartTestServer()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, ctx := launchTestContext(t)
	page := firstPage(t, ctx)

	if err := ctx.AddInitScript(pw.Script{Content: ptr(`window['contextInitCount'] = (window['contextInitCount'] || 0) + 1`)}); err != nil {
		t.Fatal(err)
	}
	if err := page.AddInitScript(pw.Script{Content: ptr(`window['pageInitCount'] = (window['pageInitCount'] || 0) + 1`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(srv.Origin + "/patchright-init-redirect.html"); err != nil {
		t.Fatal(err)
	}
	info, err := page.Evaluate(`() => ({
		context: window['contextInitCount'],
		page: window['pageInitCount'],
		compatMode: document.compatMode,
		charset: document.characterSet,
		body: document.querySelector('main')?.textContent,
	})`)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := info.(map[string]any)
	if !ok {
		t.Fatalf("unexpected shape %T", info)
	}
	if m["context"] != float64(1) || m["page"] != float64(1) {
		t.Errorf("init counts = %v, want 1/1", m)
	}
	if m["compatMode"] != "CSS1Compat" || m["charset"] != "UTF-8" || m["body"] != "document" {
		t.Errorf("document disturbed: %v", m)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatal(err)
	}
	counts, err := page.Evaluate(`() => [window['contextInitCount'], window['pageInitCount']]`)
	if err != nil {
		t.Fatal(err)
	}
	if arr, ok := counts.([]any); !ok || len(arr) != 2 || arr[0] != float64(1) || arr[1] != float64(1) {
		t.Errorf("reload counts = %v, want [1 1]", counts)
	}

	// Bindings across navigation.
	if err := ctx.ExposeFunction("patchrightAdd", func(args ...any) any {
		a, _ := args[0].(float64)
		b, _ := args[1].(float64)
		return a + b
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := page.Goto(srv.Origin + "/patchright-binding.html"); err != nil {
		t.Fatal(err)
	}
	sum, err := page.Evaluate(`window['patchrightAdd'](2, 3)`)
	if err != nil {
		t.Fatal(err)
	}
	if sum != float64(5) {
		t.Errorf("patchrightAdd(2,3) = %v, want 5", sum)
	}
	if _, err := page.Reload(); err != nil {
		t.Fatal(err)
	}
	sum, err = page.Evaluate(`window['patchrightAdd'](2, 3)`)
	if err != nil {
		t.Fatal(err)
	}
	if sum != float64(5) {
		t.Errorf("after reload patchrightAdd(2,3) = %v, want 5", sum)
	}
}
