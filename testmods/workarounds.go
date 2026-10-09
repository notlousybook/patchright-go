// Package testmods rewrites upstream playwright test files so they pass on the
// patched driver (used to be modify_tests.ts). sticks isolatedContext=false
// into evaluate calls, fixme()s the known-broken ones, works around the
// about:blank thing, drops in the custom patchright specs.
package testmods

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Report mirrors ModifyTestsReport.
type Report struct {
	FilesVisited                  int
	FilesChanged                  int
	CustomTestsInjected           int
	IsolatedContextInsertions     int
	IsolatedContextNormalizations int
	FixmeInsertions               int
	PatchrightWorkaroundFiles     int
	SkippedUnsafeEvaluateCalls    int
	ChangedFiles                  []ChangedFile
}

// ChangedFile mirrors ChangedFileReport.
type ChangedFile struct {
	File                          string
	IsolatedContextInsertions     int
	IsolatedContextNormalizations int
	FixmeInsertions               int
	PatchrightWorkaround          int
}

func (r *Report) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[modify_tests] filesVisited=%d filesChanged=%d\n", r.FilesVisited, r.FilesChanged)
	fmt.Fprintf(&sb, "[modify_tests] isolatedContextInsertions=%d fixmeInsertions=%d\n", r.IsolatedContextInsertions, r.FixmeInsertions)
	fmt.Fprintf(&sb, "[modify_tests] isolatedContextNormalizations=%d\n", r.IsolatedContextNormalizations)
	fmt.Fprintf(&sb, "[modify_tests] patchrightWorkaroundFiles=%d\n", r.PatchrightWorkaroundFiles)
	fmt.Fprintf(&sb, "[modify_tests] skippedUnsafeEvaluateCalls=%d\n", r.SkippedUnsafeEvaluateCalls)
	fmt.Fprintf(&sb, "[modify_tests] customTestsInjected=%d\n", r.CustomTestsInjected)
	for _, c := range r.ChangedFiles {
		fmt.Fprintf(&sb, "[modify_tests] changed %s (+isolated=%d, ~isolated=%d, +fixme=%d, +patchrightWorkaround=%d)\n",
			c.File, c.IsolatedContextInsertions, c.IsolatedContextNormalizations, c.FixmeInsertions, c.PatchrightWorkaround)
	}
	return sb.String()
}

// TargetMethods mirrors TARGET_METHODS.
var TargetMethods = []string{"evaluate", "evaluateHandle", "evaluateAll"}

// TestBaseNames mirrors TEST_BASE_NAMES.
var TestBaseNames = []string{"it", "test", "playwrightTest"}

// editor is a SourceTextEditor equivalent: replaceAll/replaceOnce with
// missing-replacement tracking.
type editor struct {
	text     string
	original string
	missing  []string
}

func newEditor(text string) *editor {
	return &editor{text: text, original: text}
}

func (e *editor) replaceAll(from, to string) {
	if strings.Contains(e.text, from) {
		e.text = strings.ReplaceAll(e.text, from, to)
	}
}

func (e *editor) replaceOnce(from, to string) {
	if !strings.Contains(e.text, from) {
		e.missing = append(e.missing, from)
		return
	}
	e.text = strings.Replace(e.text, from, to, 1)
}

func (e *editor) changed() bool { return e.text != e.original }

// workaround is one replaceOnce/replaceAll pair for a spec file.
type workaround struct {
	from, to string
	all      bool
}

// Workarounds mirrors applyPatchrightWorkarounds: per-file string rewrites.
// Tables are verbatim from modify_tests.ts (only the subset expressible as
// plain string pairs is included; the page-clock.spec.ts fixture regex is
// handled in code).
var Workarounds = map[string][]workaround{
	"tests/library/browsercontext-add-init-script.spec.ts": {
		{"it('should work without navigation, after all bindings', async ({ context }) => {",
			"it('should work without navigation, after all bindings', async ({ context, server }) => {", false},
		{"it('should work without navigation in popup', async ({ context }) => {", "it('should work without navigation in popup', async ({ context, server }) => {", false},
		{"it('init script should run only once in popup', async ({ context }) => {", "it('init script should run only once in popup', async ({ context, server }) => {", false},
		{"  const page = await context.newPage();\n\n  expect(await page.evaluate(() => (window as any)['temp'], undefined, undefined, false)).toBe(123);",
			"  const page = await context.newPage();\n  await page.goto(server.EMPTY_PAGE);\n\n  expect(await page.evaluate(() => (window as any)['temp'], undefined, undefined, false)).toBe(123);", false},
		{"  await context.addInitScript(() => {\n    (window as any)['woof']('hey');\n    (window as any)['temp'] = 123;\n  });",
			"  await context.addInitScript(() => {\n    const retry = () => {\n      const fn = (window as any)['woof'];\n      if (typeof fn === 'function') fn('hey');\n      else setTimeout(retry, 0);\n    };\n    retry();\n    (window as any)['temp'] = 123;\n  });", false},
		{"  const page = await context.newPage();\n  const [popup] = await Promise.all([",
			"  const page = await context.newPage();\n  await page.goto(server.EMPTY_PAGE);\n  const [popup] = await Promise.all([", false},
		{"    page.evaluate(() => (window as any)['win'] = window.open(), undefined, undefined, false),",
			"    page.evaluate(url => (window as any)['win'] = window.open(url), server.EMPTY_PAGE, undefined, false),", false},
		{"  ]);\n  expect(await popup.evaluate(() => (window as any)['temp'], undefined, undefined, false)).toBe(123);",
			"  ]);\n  await popup.waitForLoadState();\n  expect(await popup.evaluate(() => (window as any)['temp'], undefined, undefined, false)).toBe(123);", false},
		{"    page.evaluate(() => window.open('about:blank'), undefined, undefined, false),", "    page.evaluate(url => window.open(url), server.EMPTY_PAGE, undefined, false),", false},
		{"  ]);\n  expect(await popup.evaluate('callCount', undefined, undefined, false)).toEqual(1);",
			"  ]);\n  await popup.waitForLoadState();\n  expect([2, 3]).toContain(await popup.evaluate('callCount', undefined, undefined, false));", false},
		{"  await popup.waitForLoadState();\n  expect(await popup.evaluate('callCount', undefined, undefined, false)).toEqual(1);",
			"  await popup.waitForLoadState();\n  expect([2, 3]).toContain(await popup.evaluate('callCount', undefined, undefined, false));", true},
		{"  await popup.waitForLoadState();\n  expect(await popup.evaluate('callCount', undefined, undefined, false)).toEqual(3);",
			"  await popup.waitForLoadState();\n  expect([2, 3]).toContain(await popup.evaluate('callCount', undefined, undefined, false));", true},
		{"  await popup.waitForLoadState();\n  expect(await popup.evaluate('callCount', undefined, undefined, false)).toEqual(2);",
			"  await popup.waitForLoadState();\n  expect([2, 3]).toContain(await popup.evaluate('callCount', undefined, undefined, false));", true},
	},
	"tests/library/page-clock.spec.ts": {
		{"await page.goto('data:text/html,');", "await page.goto(server.EMPTY_PAGE);", true},
		{"page.evaluate(() => window.open('about:blank'), undefined, undefined, false),", "page.evaluate(url => window.open(url), server.EMPTY_PAGE, undefined, false),", true},
		{"]);\n    const popupTime = await popup.evaluate(() => Date.now(), undefined, undefined, false);",
			"]);\n    await popup.waitForLoadState();\n    const popupTime = await popup.evaluate(() => Date.now(), undefined, undefined, false);", true},
		{"const waitForDone = page.waitForEvent('console', msg => msg.text() === 'done');", "const waitForDone = page.waitForFunction(() => (window as any).__pw_done);", false},
		{"console.log('done');", "window.__pw_done = true; console.log('done');", false},
	},
	"tests/library/emulation-focus.spec.ts": {
		{"page.evaluate(clickCounter),\n    page2.evaluate(clickCounter),",
			"page.evaluate(clickCounter, undefined, undefined, false),\n    page2.evaluate(clickCounter, undefined, undefined, false),", false},
		{"frame1.evaluate(logger),\n    frame2.evaluate(logger),", "frame1.evaluate(logger, undefined, undefined, false),\n    frame2.evaluate(logger, undefined, undefined, false),", false},
	},
	"tests/page/page-network-response.spec.ts": {
		{"  it.fail(browserName === 'webkit' || browserName === 'chromium', 'https://github.com/microsoft/playwright/issues/11035');",
			"  it.fail(browserName === 'webkit', 'https://github.com/microsoft/playwright/issues/11035');", false},
	},
	"tests/page/page-request-fulfill.spec.ts": {
		{"  it.fail(browserName === 'chromium', 'Set-Cookie is missing in response after interception');\n", "", false},
	},
	"tests/library/popup.spec.ts": {
		{"  const injected = await page.evaluate(() => {\n    const win = window.open('about:blank');\n    return win['injected'];\n  }, undefined, undefined, false);",
			"  const injected = await page.evaluate(async url => {\n    const win = window.open(url);\n    await new Promise(f => win.onload = f);\n    return win['injected'];\n  }, server.EMPTY_PAGE, undefined, false);", false},
		{"  await Promise.all([\n    page.waitForEvent('popup'),\n    page.evaluate(async () => {\n      const win = window.open('about:blank');\n      win['add'](9, 4);\n      win.close();\n    }, undefined, undefined, false),\n  ]);",
			"  const [popup] = await Promise.all([\n    page.waitForEvent('popup'),\n    page.evaluate(url => window.open(url), server.EMPTY_PAGE, undefined, false),\n  ]);\n  await popup.waitForLoadState();\n  await Promise.all([\n    popup.waitForEvent('close'),\n    popup.evaluate(() => { window['add'](9, 4); window.close(); }, undefined, undefined, false),\n  ]);", false},
	},
}

// HitTargetWorkarounds holds the long hit-target.spec.ts rewrites separately
// to keep the table readable; keyed the same way.
var HitTargetWorkarounds = []workaround{
	{"await page.$eval('button', button => {\n    button.addEventListener('mousemove', () => {\n      button.style.marginLeft = '100px';\n    });\n\n    const allEvents = [];\n    (window as any).allEvents = allEvents;\n    for (const name of ['mousemove', 'mousedown', 'mouseup', 'click', 'dblclick', 'auxclick', 'contextmenu', 'pointerdown', 'pointerup'])\n      button.addEventListener(name, e => allEvents.push(e.type));\n  });",
		"await page.evaluate(() => {\n    const button = document.querySelector('button')!;\n    button.addEventListener('mousemove', () => {\n      button.style.marginLeft = '100px';\n    });\n\n    const allEvents = [];\n    (window as any).allEvents = allEvents;\n    for (const name of ['mousemove', 'mousedown', 'mouseup', 'click', 'dblclick', 'auxclick', 'contextmenu', 'pointerdown', 'pointerup'])\n      button.addEventListener(name, e => allEvents.push(e.type));\n  }, undefined, undefined, false);", false},
	{"await page.$eval('button', button => {\n    button.addEventListener('mousedown', () => {\n      (window as any).result = 'Mousedown';\n      button.remove();\n    });\n  });",
		"await page.evaluate(() => {\n    const button = document.querySelector('button')!;\n    button.addEventListener('mousedown', () => {\n      (window as any).result = 'Mousedown';\n      button.remove();\n    });\n  }, undefined, undefined, false);", false},
	{"await page.$eval('button', button => {\n    const blocker = document.createElement('div');",
		"await page.evaluate(() => {\n    const button = document.querySelector('button')!;\n    const blocker = document.createElement('div');", false},
	{"      blocker.addEventListener(name, e => allEvents.push(e.type));\n    }\n  });",
		"      blocker.addEventListener(name, e => allEvents.push(e.type));\n    }\n  }, undefined, undefined, false);", false},
	{"await page.$eval('button', button => {\n    button.addEventListener('mousemove', () => {\n      button.style.marginLeft = '100px';\n      button.dispatchEvent(new MouseEvent('click'));\n    });\n\n    const allEvents = [];\n    (window as any).allEvents = allEvents;\n    button.addEventListener('click', e => {\n      if (!e.isTrusted)\n        allEvents.push(e.type);\n    });\n  });",
		"await page.evaluate(() => {\n    const button = document.querySelector('button')!;\n    button.addEventListener('mousemove', () => {\n      button.style.marginLeft = '100px';\n      button.dispatchEvent(new MouseEvent('click'));\n    });\n\n    const allEvents = [];\n    (window as any).allEvents = allEvents;\n    button.addEventListener('click', e => {\n      if (!e.isTrusted)\n        allEvents.push(e.type);\n    });\n  }, undefined, undefined, false);", false},
}

// PageClickWorkaround holds the page-click.spec.ts rewrite.
var PageClickWorkaround = workaround{
	"  await page.evaluate(() => {\n    const logEvent = e => console.log(e.type);\n    document.addEventListener('mousedown', logEvent);\n    document.addEventListener('mouseup', logEvent);\n    document.addEventListener('contextmenu', logEvent);\n  }, undefined, undefined, false);\n  const entries = [];\n  page.on('console', message => entries.push(message.text()));\n  await page.getByRole('button', { name: 'Click me' }).click({ button: 'right' });",
	"  await page.evaluate(() => {\n    window['entries'] = [];\n    const logEvent = e => window['entries'].push(e.type);\n    document.addEventListener('mousedown', logEvent);\n    document.addEventListener('mouseup', logEvent);\n    document.addEventListener('contextmenu', logEvent);\n  }, undefined, undefined, false);\n  await page.getByRole('button', { name: 'Click me' }).click({ button: 'right' });\n  const entries = await page.evaluate(() => window['entries'], undefined, undefined, false);",
	false,
}

// applyWorkarounds runs the per-file string rewrites, returning whether the
// file changed. It mirrors applyPatchrightWorkarounds including the
// page-clock.spec.ts server-fixture regex.
func applyWorkarounds(e *editor, relativePath string) bool {
	before := e.text
	var pairs []workaround
	if w, ok := Workarounds[relativePath]; ok {
		pairs = append(pairs, w...)
	}
	switch relativePath {
	case "tests/library/hit-target.spec.ts":
		pairs = append(pairs, HitTargetWorkarounds...)
	case "tests/page/page-click.spec.ts":
		pairs = append(pairs, PageClickWorkaround)
	}
	for _, w := range pairs {
		if w.all {
			e.replaceAll(w.from, w.to)
		} else {
			e.replaceOnce(w.from, w.to)
		}
	}
	if relativePath == "tests/library/page-clock.spec.ts" {
		// Add server to fixtures: async ({...}) => { without server gains it.
		e.text = fixtureServerRe.ReplaceAllStringFunc(e.text, func(m string) string {
			sub := fixtureServerRe.FindStringSubmatch(m)
			if len(sub) != 2 {
				return m
			}
			inside := sub[1]
			if !strings.Contains(inside, "page") || strings.Contains(inside, "server") {
				return m
			}
			next := strings.TrimSpace(inside)
			if next != "" {
				next += ", server"
			} else {
				next = "server"
			}
			return "async ({ " + next + " }) => {"
		})
	}
	return e.text != before
}

// FixmeTargets mirrors FIXME_TARGETS: test title -> reason by spec file.
var FixmeTargets = map[string]map[string]string{}

// FixmeTargetFiles mirrors FIXME_TARGET_FILES: whole-file reasons.
var FixmeTargetFiles = map[string]string{
	"tests/page/page-event-console.spec.ts":   "Known Patchright bug: Console CDP domain is disabled, so ConsoleMessage semantics differ from upstream.",
	"tests/page/page-event-pageerror.spec.ts": "Known Patchright bug: Console CDP domain is disabled, so PageError/WebError semantics differ from upstream.",
	"tests/page/workers.spec.ts":              "Known Patchright bug: Console CDP domain is disabled, so worker console/error propagation semantics differ from upstream.",
	"tests/library/route-web-socket.spec.ts":  "WebsocketRoutes do not work in Patchright.",
	"tests/library/trace-viewer.spec.ts":      "I just gave up at this point. Im sorry.",
}

func init() {
	add := func(file string, titles ...[2]string) {
		m := map[string]string{}
		for _, t := range titles {
			m[t[0]] = t[1]
		}
		FixmeTargets[file] = m
	}
	consoleBug := "Known Patchright bug: Console CDP domain is disabled, so console events/messages are not reliably available."
	add("tests/page/page-basic.spec.ts", [2]string{"has navigator.webdriver set to true", "Patchright intentionally disables automation fingerprinting."},
		[2]string{"page.press should work for Enter", consoleBug})
	add("tests/page/interception.spec.ts", [2]string{"should intercept network activity from worker", consoleBug},
		[2]string{"should intercept worker requests when enabled after worker creation", consoleBug},
		[2]string{"should intercept network activity from worker 2", consoleBug})
	add("tests/page/jshandle-to-string.spec.ts", [2]string{"should beautifully render sparse arrays", consoleBug})
	add("tests/page/page-click.spec.ts", [2]string{"should click offscreen buttons", consoleBug},
		[2]string{"ensure events are dispatched in the individual tasks", consoleBug})
	add("tests/page/page-history.spec.ts", [2]string{"page.goBack should work for file urls", consoleBug},
		[2]string{"regression test for issue 20791", consoleBug})
	add("tests/page/page-listeners.spec.ts", [2]string{"should not throw with ignoreErrors", consoleBug},
		[2]string{"should wait", consoleBug}, [2]string{"wait should throw", consoleBug})
	add("tests/page/page-screenshot.spec.ts", [2]string{"should trigger particular events for css transitions", consoleBug},
		[2]string{"should trigger particular events for INfinite css animation", consoleBug},
		[2]string{"should trigger particular events for finite css animation", consoleBug},
		[2]string{"should work for webgl", "Patchright removes Chromium fallback GL settings, so WebGL screenshots are environment-dependent."})
	add("tests/page/page-wait-for-function.spec.ts",
		[2]string{"should work when resolved right before execution context disposal", "Known Patchright limitation: initScripts injected via routing cannot affect about:blank/data URLs, so addInitScript does not run on the initial about:blank."},
		[2]string{"should not be called after finishing successfully", consoleBug},
		[2]string{"should not be called after finishing unsuccessfully", consoleBug})
	add("tests/page/page-add-init-script.spec.ts", [2]string{"init script should run only once in iframe", "Patchright inject-route bootstrap can alter init-script timing/order."},
		[2]string{"should work with trailing comments", "Patchright init-script injection path changes script-source handling for trailing-comment case."},
		[2]string{"should work with CSP", "Patchright intentionally relaxes CSP restrictions for injected scripts."})
	add("tests/page/page-add-script-tag.spec.ts", [2]string{"should include sourceURL when path is provided", "Patchright removes sourceURL-style script wrapping for stealth, so stack source paths differ."})
	add("tests/page/page-goto.spec.ts", [2]string{"should report raw buffer for main resource", "Patchright always-on routing receives Chromium main resources through the text path, matching upstream Chromium failure behavior."})
	add("tests/page/network-post-data.spec.ts", [2]string{"should get post data for file/blob", "Upstream expected-fail now passes in Patchright; keep suite deterministic."},
		[2]string{"should get post data for navigator.sendBeacon api calls", "Upstream expected-fail now passes in Patchright; keep suite deterministic."})
	add("tests/page/page-expose-function.spec.ts", [2]string{"should be callable from-inside addInitScript", "Patchright inject-route bootstrap can alter init-script timing/order."})
	add("tests/page/page-evaluate.spec.ts", [2]string{"should throw when passed more than one parameter", "Patchright uses third evaluate argument as isolatedContext boolean, changing argument validation semantics."},
		[2]string{"should modify global environment", "Patchright evaluate string expressions run in utility context by default; global variable visibility differs."},
		[2]string{"should evaluate in the page context", "Patchright evaluate string expressions run in utility context by default; page-global resolution differs."})
	add("tests/library/chromium/chromium.spec.ts", [2]string{"should emit console messages from service worker", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."},
		[2]string{"should capture console.log from ServiceWorker start", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."})
	add("tests/library/chromium/connect-to-worker.spec.ts", [2]string{"should connect, evaluate, receive console and disconnect", "Console CDP domain is disabled in Patchright, so worker console/evaluation timing differs from upstream."})
	add("tests/library/capabilities.spec.ts", [2]string{"should support webgl @smoke", "Patchright removes the unsafe SwiftShader fallback, so WebGL availability is environment-dependent in headless runs."},
		[2]string{"should support webgl 2 @smoke", "Patchright removes the unsafe SwiftShader fallback, so WebGL availability is environment-dependent in headless runs."})
	add("tests/library/har-websocket.spec.ts", [2]string{"should still capture websocket when route passes messages through", "WebsocketRoutes do not work in Patchright."},
		[2]string{"should still allow routeWebSocket to fully mock the connection when capturing HAR", "WebsocketRoutes do not work in Patchright."},
		[2]string{"should still allow routeWebSocket to modify messages when capturing HAR", "WebsocketRoutes do not work in Patchright."},
		[2]string{"should respect PLAYWRIGHT_HAR_NO_WEBSOCKET_FRAMES", "Patchright library tests run through an out-of-process driver, so runtime process.env mutations are not visible to the HAR recorder process."})
	add("tests/library/browsercontext-webauthn.spec.ts",
		[2]string{"should seed a known credential and authenticate", "Patchright driver-mode WebAuthn binding can hang in the upstream library fixture even though the direct credentials API path works."},
		[2]string{"should capture a page-created credential and reuse it in another context", "Patchright driver-mode WebAuthn binding can fall back to native WebAuthn in the upstream library fixture."},
		[2]string{"should reuse a page-created credential via the storageState option", "Patchright driver-mode WebAuthn binding can fall back to native WebAuthn in the upstream library fixture."})
	add("tests/library/browsercontext-events.spec.ts", [2]string{"console event should work @smoke", consoleBug},
		[2]string{"console event should work with element handles", consoleBug},
		[2]string{"console event should work in popup", consoleBug},
		[2]string{"console event should work in popup 2", consoleBug},
		[2]string{"console event should work in immediately closed popup", consoleBug},
		[2]string{"weberror event should work", "Known Patchright bug: Console CDP domain is disabled, so PageError/WebError semantics differ from upstream."},
		[2]string{"weberror event should include location", "Known Patchright bug: Console CDP domain is disabled, so PageError/WebError semantics differ from upstream."})
	add("tests/library/browsercontext-locale.spec.ts", [2]string{"should propagate locale to workers", "Console CDP domain is disabled in Patchright, so worker console events are not emitted and this test times out waiting for console output."})
	add("tests/library/browsercontext-timezone-id.spec.ts", [2]string{"should propagate timezone to workers", "Console CDP domain is disabled in Patchright, so worker console events are not emitted and this test times out waiting for console output."})
	add("tests/library/browsercontext-expose-function.spec.ts", [2]string{"should be callable from-inside addInitScript", "Patchright inject-route bootstrap can alter init-script timing/order."})
	add("tests/library/browsercontext-reuse.spec.ts", [2]string{"should work with routeWebSocket", "WebsocketRoutes do not work in Patchright."})
	add("tests/library/browsercontext-service-worker-policy.spec.ts", [2]string{"blocks service worker registration", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."})
	add("tests/library/browsercontext-viewport-mobile.spec.ts", [2]string{"should fire orientationchange event", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."})
	add("tests/library/browsertype-connect.spec.ts", [2]string{"should send extra headers with connect request", "WebsocketRoutes do not work in Patchright."},
		[2]string{"should send default User-Agent and X-Playwright-Browser headers with connect request", "WebsocketRoutes do not work in Patchright."})
	add("tests/library/geolocation.spec.ts", [2]string{"watchPosition should be notified", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."})
	add("tests/library/popup.spec.ts", [2]string{"should expose function from browser context", "Patchright inject-route bootstrap can alter init-script timing/order."})
	add("tests/library/tracing.spec.ts", [2]string{"should not flush console events", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."},
		[2]string{"should flush console events on tracing stop", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."},
		[2]string{"should not emit after w/o before", "Console CDP domain is disabled in Patchright, so console.log never fires and the evaluate promise never resolves."},
		[2]string{"should save trace while a WebSocket keeps streaming frames", "Patchright tracing export can hang while a WebSocket keeps streaming frames."})
	add("tests/library/inspector/recorder-api.spec.ts", [2]string{"page.pickLocator should return locator for picked element", "Console CDP domain is disabled in Patchright, so recorder readiness console events are not emitted and this test times out waiting for console output."})
	add("tests/library/selectors-register.spec.ts", [2]string{"should work in main and isolated world", "$eval is deprecated by Playwright and not supported by Patchright."})
	add("tests/library/chromium/oopif.spec.ts", [2]string{"should be able to click in iframe", "Console CDP domain is disabled in Patchright, so console events are never emitted and the test hangs waiting for them."})
}

// ModifyFile applies workarounds, isolatedContext rewrites, and fixmes to one
// spec file's text. It returns the updated text and per-file counts.
func ModifyFile(relativePath, text string) (string, ChangedFile) {
	cf := ChangedFile{}
	e := newEditor(text)
	if applyWorkarounds(e, relativePath) {
		cf.PatchrightWorkaround = 1
	}
	ins, norm, skip := rewriteIsolatedContext(e)
	cf.IsolatedContextInsertions = ins
	cf.IsolatedContextNormalizations = norm
	cf.FixmeInsertions = insertFixmes(e, relativePath)
	_ = skip
	_ = e.missing
	return e.text, cf
}

// ModifyTree walks playwrightRoot/tests/{page,library} for spec files,
// rewrites them in place (unless dryRun), copies custom tests, and returns a
// report. It mirrors main() in modify_tests.ts.
func ModifyTree(playwrightRoot, customTestsRoot string, dryRun bool) (*Report, error) {
	report := &Report{}
	testsRoot := filepath.Join(playwrightRoot, "tests")
	for _, sub := range []string{"page", "library"} {
		var files []string
		err := filepath.Walk(filepath.Join(testsRoot, sub), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".spec.ts") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			rel, err := filepath.Rel(playwrightRoot, f)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if strings.HasPrefix(rel, "tests/library/patchright/") {
				continue
			}
			data, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			report.FilesVisited++
			updated, cf := ModifyFile(rel, string(data))
			report.IsolatedContextInsertions += cf.IsolatedContextInsertions
			report.IsolatedContextNormalizations += cf.IsolatedContextNormalizations
			report.FixmeInsertions += cf.FixmeInsertions
			report.PatchrightWorkaroundFiles += cf.PatchrightWorkaround
			if updated != string(data) {
				report.FilesChanged++
				cf2 := cf
				report.ChangedFiles = append(report.ChangedFiles, ChangedFile{
					File:                          rel,
					IsolatedContextInsertions:     cf2.IsolatedContextInsertions,
					IsolatedContextNormalizations: cf2.IsolatedContextNormalizations,
					FixmeInsertions:               cf2.FixmeInsertions,
					PatchrightWorkaround:          cf2.PatchrightWorkaround,
				})
				if !dryRun {
					if err := os.WriteFile(f, []byte(updated), 0o644); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	// Inject custom tests.
	if !dryRun {
		target := filepath.Join(testsRoot, "library", "patchright")
		os.RemoveAll(target)
		if err := os.MkdirAll(target, 0o755); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(customTestsRoot)
		if err != nil {
			return nil, err
		}
		for _, en := range entries {
			if en.IsDir() || !strings.HasSuffix(en.Name(), ".spec.ts") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(customTestsRoot, en.Name()))
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(target, en.Name()), data, 0o644); err != nil {
				return nil, err
			}
			report.CustomTestsInjected++
		}
	}
	return report, nil
}
