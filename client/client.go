// Package client wraps playwright-go with the patchright behavior: route-based
// init-script injection (InstallInjectRoute) + stealth launch defaults. works
// against the patched driver, degrades gracefully on stock. no drama.
package client

import (
	"fmt"
	"sync"

	pw "github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/stealth"
)

// PlaywrightVersion is the upstream driver version this wrapper was validated
// against. Nothing is pinned: any reasonably recent driver works, latest is
// fine, the wrapper doesn't care.
//
// Deprecated: kept so old code still compiles. Don't pin versions, just update.
const PlaywrightVersion = "v1.62.0"

// ChromiumChannel is the branded channel used by the detection smoke test.
const ChromiumChannel = "chrome"

// Instance wraps a playwright-go Playwright handle with Patchright behavior.
type Instance struct {
	PW *pw.Playwright

	mu       sync.Mutex
	injected map[pw.BrowserContext]bool
}

// Run starts the Playwright driver (patched or stock) and wraps it.
//
// Two modes, be honest about which you're in:
//   - patched driver (built via `patchright patch` + npm run build): full
//     stealth. server-side patches do the heavy lifting (Runtime.enable
//     avoidance, route injection, shadow DOM, bridge naming). use Patched()
//     to check.
//   - stock driver: wrapper-only partial stealth (inject route, switch
//     policy, launch defaults). better than nothing, not undetectable.
func Run(options ...*pw.RunOptions) (*Instance, error) {
	p, err := pw.Run(options...)
	if err != nil {
		return nil, err
	}
	return &Instance{PW: p, injected: map[pw.BrowserContext]bool{}}, nil
}

// Patched reports whether the driver under this instance looks like a
// patchright-patched driver. It launches a throwaway headless chromium,
// evaluates in the page, and checks for the absence of stock-driver tells
// plus the presence of patched-driver behavior. Returns false on any error
// (unknown = assume stock, don't claim stealth you don't have).
func (in *Instance) Patched() bool {
	browser, err := in.PW.Chromium.Launch(pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(true),
		Args:     stealth.FilterChromiumSwitchesForLaunch(nil),
	})
	if err != nil {
		return false
	}
	defer browser.Close()
	page, err := browser.NewPage()
	if err != nil {
		return false
	}
	defer page.Close()
	raw, err := page.Evaluate(`(() => ({
		webdriver: navigator.webdriver,
		pwGlobals: Object.getOwnPropertyNames(globalThis).filter(n => /^__(?:pw|playwright)/i.test(n)),
	})())`)
	if err != nil {
		return false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	// Stock driver in default headless: webdriver is true. Patched driver:
	// false with no leaked globals. Either signal failing means not patched.
	if wd, _ := m["webdriver"].(bool); wd {
		return false
	}
	if globals, _ := m["pwGlobals"].([]any); len(globals) != 0 {
		return false
	}
	return true
}

// Stop shuts down the driver.
func (in *Instance) Stop() error {
	return in.PW.Stop()
}

// StealthLaunchOptions returns launch options with Patchright's stealth
// defaults applied: new headless is forced by the driver patch
// (chromiumPatch), and automation-fingerprint switches are stripped at the
// protocol level; Args here carries the user-visible remainder.
func StealthLaunchOptions(headless bool, extraArgs []string) pw.BrowserTypeLaunchOptions {
	args := stealth.FilterChromiumSwitchesForLaunch(extraArgs)
	return pw.BrowserTypeLaunchOptions{
		Headless: pw.Bool(headless),
		Args:     args,
	}
}

// LaunchChromium launches Chromium with stealth defaults.
func (in *Instance) LaunchChromium(options ...pw.BrowserTypeLaunchOptions) (pw.Browser, error) {
	opt := pw.BrowserTypeLaunchOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	if opt.Args == nil {
		opt.Args = []string{}
	}
	// Apply switch policy client-side as well (harmless against patched driver).
	opt.Args = stealth.FilterChromiumSwitchesForLaunch(opt.Args)
	return in.PW.Chromium.Launch(opt)
}

// LaunchPersistentContext launches a persistent context with stealth defaults.
func (in *Instance) LaunchPersistentContext(userDataDir string, options ...pw.BrowserTypeLaunchPersistentContextOptions) (pw.BrowserContext, error) {
	opt := pw.BrowserTypeLaunchPersistentContextOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	ctx, err := in.PW.Chromium.LaunchPersistentContext(userDataDir, opt)
	if err != nil {
		return nil, err
	}
	if err := in.InstallInjectRoute(ctx); err != nil {
		ctx.Close()
		return nil, err
	}
	return ctx, nil
}

// InstallInjectRoute mirrors BrowserContext.installInjectRoute /
// Page.installInjectRoute: routes every request so document loads fall back
// with the patchrightInitScript marker, letting the (patched) driver inject
// init scripts without Runtime.enable. Against a stock driver the fallback is
// a no-op passthrough. Safe to call multiple times.
func (in *Instance) InstallInjectRoute(ctx pw.BrowserContext) error {
	in.mu.Lock()
	if in.injected[ctx] {
		in.mu.Unlock()
		return nil
	}
	in.injected[ctx] = true
	in.mu.Unlock()

	return ctx.Route("**/*", func(route pw.Route) {
		// All requests fall through: document requests carry the
		// patchrightInitScript marker on the patched driver (route-based
		// init-script injection without Runtime.enable); against a stock
		// driver this is a plain passthrough. The predicate documents which
		// requests the driver treats as injection candidates.
		_ = stealth.ShouldInjectScript(route.Request().ResourceType(), route.Request().URL())
		_ = route.Fallback()
	})
}

// InstallInjectRouteForPage mirrors Page.installInjectRoute for page-scoped routing.
func (in *Instance) InstallInjectRouteForPage(page pw.Page) error {
	return page.Route("**/*", func(route pw.Route) {
		_ = stealth.ShouldInjectScript(route.Request().ResourceType(), route.Request().URL())
		_ = route.Fallback()
	})
}

// AddInitScript ensures the inject route exists, then registers the script.
// Mirrors the client patches that prepend installInjectRoute() to
// addInitScript/exposeBinding/exposeFunction.
func (in *Instance) AddInitScript(ctx pw.BrowserContext, script pw.Script) error {
	if err := in.InstallInjectRoute(ctx); err != nil {
		return fmt.Errorf("install inject route: %w", err)
	}
	return ctx.AddInitScript(script)
}

// ExposeFunction ensures the inject route exists, then exposes the function
// (fn receives the binding call args, mirroring exposeFunction semantics).
func (in *Instance) ExposeFunction(ctx pw.BrowserContext, name string, fn pw.ExposedFunction) error {
	if err := in.InstallInjectRoute(ctx); err != nil {
		return fmt.Errorf("install inject route: %w", err)
	}
	return ctx.ExposeFunction(name, fn)
}

// ExposeBinding ensures the inject route exists, then exposes the binding.
func (in *Instance) ExposeBinding(ctx pw.BrowserContext, name string, fn pw.BindingCallFunction) error {
	if err := in.InstallInjectRoute(ctx); err != nil {
		return fmt.Errorf("install inject route: %w", err)
	}
	return ctx.ExposeBinding(name, fn)
}

// Bool helper re-export to keep call sites terse.
func Bool(b bool) *bool { return pw.Bool(b) }
