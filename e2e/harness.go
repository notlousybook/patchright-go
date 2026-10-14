//go:build e2e

package e2e

import (
	"os"
	"testing"

	"github.com/playwright-community/playwright-go"

	"github.com/notlousybook/patchright-go/browsers"
	"github.com/notlousybook/patchright-go/client"
	"github.com/notlousybook/patchright-go/driver"
)

// harness carries the one thing every e2e test needs: which driver it actually
// ran against. Reporting it is the point — a green run should always mean
// something specific rather than "whatever happened to be on this machine".
type harness struct {
	inst *client.Instance
	ctx  playwright.BrowserContext
	page playwright.Page
	prov provenanceReport
}

type provenanceReport struct {
	driverDir   string
	source      string
	fullStealth bool
	node        string
}

// setup provisions the driver (auto, or PATCHRIGHT_DRIVER_PATH override),
// launches through the one launch seam, and reports provenance.
func setup(t *testing.T) *harness {
	t.Helper()
	if os.Getenv("PATCHRIGHT_SKIP_E2E") == "1" {
		t.Skip("PATCHRIGHT_SKIP_E2E=1")
	}
	inst, err := client.Run()
	if err != nil {
		t.Skipf("driver unavailable: %v", err)
	}
	t.Cleanup(func() { inst.Stop() })

	rep := provenanceReport{}
	if d, err := driverCacheDir(); err == nil {
		rep.driverDir = d
	}
	if src := driverSource(rep.driverDir); src != "" {
		rep.source = src.Describe()
		rep.fullStealth = src.FullStealth()
	}
	rep.node = driverNodeSource(rep.driverDir)
	t.Logf("e2e driver provenance: source=%s full_stealth=%v node=%s", rep.source, rep.fullStealth, rep.node)
	if !rep.fullStealth {
		t.Logf("e2e WARNING: partial-stealth driver; shadow-root assertions may not hold")
	}

	var picked *browsers.Found
	if os.Getenv("PATCHRIGHT_E2E_BROWSER") == "" {
		if f := browsers.Default(); f != nil && (f.Browser.Channel != "" || f.Path != "") {
			picked = f
		}
	} else {
		for _, f := range browsers.Discover() {
			if f.Browser.ID == os.Getenv("PATCHRIGHT_E2E_BROWSER") {
				picked = &f
				break
			}
		}
		if picked == nil {
			t.Skipf("forced browser %q not found", os.Getenv("PATCHRIGHT_E2E_BROWSER"))
		}
	}
	ctx, page, err := inst.LaunchStealth(picked, true)
	if err != nil {
		t.Skipf("launch failed: %v", err)
	}
	t.Cleanup(func() { ctx.Close() })
	return &harness{inst: inst, ctx: ctx, page: page, prov: rep}
}

func (h *harness) requireFullStealth(t *testing.T) {
	t.Helper()
	if !h.prov.fullStealth {
		t.Skipf("needs full stealth, got %s", h.prov.source)
	}
}

// driverCacheDir / driverSource / driverNodeSource resolve provenance through
// the driver module. Kept as vars so a test can stub them if ever needed.
var (
	driverCacheDir   = driver.CacheDir
	driverSource     = driver.Source
	driverNodeSource = driver.NodeSource
)
