// Package client is the one mode: full stealth, always.
//
// Run() auto-provisions the patched driver behind the scenes (see driver/:
// stock driver zip download + in-place bundle patching, cached per version)
// and wraps it with route-based init-script injection + stealth launch
// defaults. no manual patch step, no stock fallback, no two modes. if the
// patched driver can't be provisioned, Run errors — it will not silently
// downgrade you to stock and let detectors eat you.
package client

import (
	"fmt"
	"os"
	"sync"

	pw "github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/browsers"
	"github.com/notlousybook/patchright-go/driver"
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

// Run starts the PATCHED driver and wraps it. First call downloads the stock
// driver zip once, applies the bundle stealth transforms in place, and caches
// the result (see driver.Ensure). Pass your own &pw.RunOptions{DriverDirectory}
// only if you built a patched driver yourself via `patchright patch` —
// otherwise leave it empty and let provisioning handle it.
func Run(options ...*pw.RunOptions) (*Instance, error) {
	var opt *pw.RunOptions
	if len(options) > 0 && options[0] != nil {
		opt = options[0]
	} else {
		opt = &pw.RunOptions{}
	}
	if opt.DriverDirectory == "" {
		dir, err := driver.Ensure()
		if err != nil {
			return nil, fmt.Errorf("provision patched driver: %w", err)
		}
		opt.DriverDirectory = dir
	}
	p, err := pw.Run(opt)
	if err != nil {
		return nil, err
	}
	return &Instance{PW: p, injected: map[pw.BrowserContext]bool{}}, nil
}

// LaunchStealth is the deep launch seam: it owns the whole "give me a usable
// stealthy browser page" sequence — browser selection (channel when the
// discovered browser has one, executable path otherwise), launch with the
// stealth switch policy, inject-route installation, and page acquisition.
//
// Everything that needs to drive a browser (the demo, the e2e suites, user
// code) calls this, so a launch-behaviour change lands in exactly one module.
//
// picked may be nil to launch without local-browser discovery (driver's own
// bundled chromium).
func (in *Instance) LaunchStealth(picked *browsers.Found, headless bool) (pw.BrowserContext, pw.Page, error) {
	dir, err := os.MkdirTemp("", "patchright-*")
	if err != nil {
		return nil, nil, err
	}
	opts := pw.BrowserTypeLaunchPersistentContextOptions{
		Headless: pw.Bool(headless),
		Args:     stealth.FilterChromiumSwitchesForLaunch(nil),
	}
	if picked != nil {
		if picked.Browser.Channel != "" {
			opts.Channel = pw.String(picked.Browser.Channel)
		} else {
			opts.ExecutablePath = pw.String(picked.Path)
		}
	}
	ctx, err := in.PW.Chromium.LaunchPersistentContext(dir, opts)
	if err != nil {
		os.RemoveAll(dir)
		return nil, nil, err
	}
	// The temp dir is owned by the context for its lifetime; playwright keeps
	// using it, so it is intentionally not removed here.
	if err := in.InstallInjectRoute(ctx); err != nil {
		ctx.Close()
		os.RemoveAll(dir)
		return nil, nil, err
	}
	pages := ctx.Pages()
	var page pw.Page
	if len(pages) > 0 {
		page = pages[0]
	} else {
		page, err = ctx.NewPage()
		if err != nil {
			ctx.Close()
			os.RemoveAll(dir)
			return nil, nil, err
		}
	}
	return ctx, page, nil
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
