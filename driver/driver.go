// Package driver makes patchright-go one mode: full stealth, always.
//
// how it works: on first use it downloads the stock playwright driver zip
// (same CDN playwright-go uses), unzips it into its own cache dir, then
// applies the patchright transforms to the extracted bundle IN PLACE, pure go,
// no npm, no node, no build step. every later run just reuses the patched dir.
// hand the resulting DriverDirectory to playwright-go via RunOptions and the
// client talks to a patched driver without anyone doing anything manually.
//
// the transforms applied here mirror patcher/ but target the BUILT bundle
// files (package/lib/*.js), not the TS sources. same stealth, different layer.
package driver

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// execLookPath is exec.LookPath, indirected for testability.
var execLookPath = exec.LookPath

// Version is the upstream playwright driver version we provision, and it is
// NOT free-form: playwright-go v0.6000.0 hard-codes driver CLI 1.60.0 and
// refuses to launch anything else (isUpToDateDriver checks the printed
// version), and upstream's patchright-core npm package is published for
// 1.60.0 but not 1.62.0. So 1.60.0 it is until the binding is bumped.
//
// If you bump the playwright-go dependency, bump this to match AND check
// patchright-core actually publishes that version (npm view patchright-core).
const Version = "v1.60.0"

// ModuleVersion is this module's own release line, used to locate the
// prebuilt driver asset tag (v + ModuleVersion). Bump on release.
const ModuleVersion = "1.1.0"

// mirrors, same as playwright-go's.
var mirrors = []string{
	"https://playwright.azureedge.net",
	"https://playwright-akamai.azureedge.net",
	"https://playwright-verizon.azureedge.net",
}

func platform() string {
	switch runtime.GOOS {
	case "windows":
		return "win32_x64"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-arm64"
		}
		return "mac"
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "linux-arm64"
		}
		return "linux"
	}
	return ""
}

// CacheDir returns our patched-driver cache root:
// <usercache>/patchright-go/<driver-version>/<platform>/
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "patchright-go", strings.TrimPrefix(Version, "v"), platform()), nil
}

// Provenance names the strategy that produced a patched driver, in descending
// order of stealth coverage. Printed by the demo, handy for bug reports.
type Provenance string

const (
	// SourceReleaseAsset: our CI-built asset (fully patched, bundled node).
	SourceReleaseAsset Provenance = "release-asset"
	// SourceNpmOverlay: stock CDN zip + upstream patchright-core overlay
	// (fully patched, bundled node).
	SourceNpmOverlay Provenance = "npm-overlay"
	// SourceNpmOnly: upstream patchright-core alone, no CDN (fully patched,
	// needs a system node).
	SourceNpmOnly Provenance = "npm-only"
	// SourceBundleTransforms: stock + our own bundle string transforms
	// (partial stealth: the big leaks, no protocol/shadow-root work).
	SourceBundleTransforms Provenance = "bundle-transforms"
)

// stampFile holds "<hash>\n<provenance>\n". The hash covers version +
// platform + transform revision + strategy, so a strategy change re-provisions.
func stampFile(dir string) string {
	return filepath.Join(dir, ".patchright-stamp")
}

// stampContent hashes version + platform + patcher revision + strategy so a
// new patchright-go release re-patches instead of reusing a stale bundle.
func stampContent(source Provenance) string {
	sum := sha256.Sum256([]byte(Version + "|" + platform() + "|" + string(source) + "|" + bundlePatchRev))
	return hex.EncodeToString(sum[:])
}

// bundlePatchRev bumps whenever bundleTransforms changes.
const bundlePatchRev = "1"

// Ensure returns a DriverDirectory holding a PATCHED driver, downloading and
// patching on first use. Pass through to playwright-go RunOptions:
//
//	pw.Run(&pw.RunOptions{DriverDirectory: dir})
func Ensure() (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	if ok, _ := isPatched(dir); ok {
		return dir, nil
	}
	if err := provision(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// MustEnsure is Ensure that panics on error (for init-time setup).
func MustEnsure() string {
	dir, err := Ensure()
	if err != nil {
		panic(err)
	}
	return dir
}

func isPatched(dir string) (bool, error) {
	data, err := os.ReadFile(stampFile(dir))
	if err != nil {
		return false, nil
	}
	// cli.js must exist too (a half-extracted dir is not patched).
	if _, err := os.Stat(filepath.Join(dir, "package", "cli.js")); err != nil {
		return false, nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return false, nil
	}
	// The stamp records the source that produced it; replay that strategy's
	// hash so switching strategies (or a strategy failing differently)
	// invalidates the cache instead of serving a lesser driver.
	var source Provenance
	if Source(dir) != "" {
		source = Source(dir)
	}
	return lines[0] == stampContent(source), nil
}

// ProvisionTo provisions a patched driver into an explicit directory.
// It's Ensure for a caller-chosen dir (CI uses it to build release assets).
func ProvisionTo(dir string) error {
	return provision(dir)
}

func provision(dir string) error {
	// start clean: a stale dir poisons everything.
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// 1. Our own release asset (CI-built, fully patched, bundled node).
	if body, err := downloadReleaseAsset(); err == nil {
		if err := unzip(body, dir); err == nil && verifyBundle(dir) == nil {
			return finish(dir, SourceReleaseAsset)
		}
		reset(dir)
	}

	// 2. Stock driver zip from the CDN (gives us the bundled node binary)
	//    with upstream's patched package/ tree overlaid on top.
	//    FULL stealth + bundled node.
	if body, err := downloadDriver(); err == nil {
		if err := unzip(body, dir); err == nil {
			if err := fetchPatchrightCoreTarball(filepath.Join(dir, "package")); err == nil && verifyBundle(dir) == nil {
				return finish(dir, SourceNpmOverlay)
			}
		}
		reset(dir)
	}

	// 3. CDN outage? npm alone still works: the fully patched package/ tree
	//    needs no CDN, only a node runtime, which comes from the system.
	//    This is why we're more robust than stock playwright-go, which dies
	//    the moment its driver CDN is down.
	if err := fetchPatchrightCoreTarball(filepath.Join(dir, "package")); err == nil && verifyBundle(dir) == nil {
		return finish(dir, SourceNpmOnly)
	}
	reset(dir)

	// 4. Last resort: stock CDN + our own in-place bundle transforms.
	//    Partial stealth, but works with no npm and no upstream tarball.
	body, err := downloadDriver()
	if err != nil {
		return err
	}
	if err := unzip(body, dir); err != nil {
		reset(dir)
		return err
	}
	if err := patchBundle(dir); err != nil {
		reset(dir)
		return err
	}
	return finish(dir, SourceBundleTransforms)
}

// finish stamps the dir and makes sure a usable node runtime is wired up:
// bundled when the CDN path ran, system node otherwise (via
// PLAYWRIGHT_NODEJS_PATH, which playwright-go honors).
func finish(dir string, source Provenance) error {
	if err := ensureNode(dir); err != nil {
		return err
	}
	return stamp(dir, source)
}

// reset empties and recreates dir for the next strategy.
func reset(dir string) {
	os.RemoveAll(dir)
	os.MkdirAll(dir, 0o755)
}

func stamp(dir string, source Provenance) error {
	return os.WriteFile(stampFile(dir), []byte(stampContent(source)+"\n"+string(source)+"\n"), 0o644)
}

// Source reads the strategy used to provision dir, or "" when unknown.
func Source(dir string) Provenance {
	data, err := os.ReadFile(stampFile(dir))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		return ""
	}
	return Provenance(lines[1])
}

// nodeFileName is the bundled node binary inside a driver dir.
func nodeFileName() string {
	if runtime.GOOS == "windows" {
		return "node.exe"
	}
	return "node"
}

// hasBundledNode reports whether dir ships its own node runtime.
func hasBundledNode(dir string) bool {
	fi, err := os.Stat(filepath.Join(dir, nodeFileName()))
	return err == nil && !fi.IsDir()
}

// ensureNode guarantees a runnable node for the driver. Order:
// bundled node in the dir, existing PLAYWRIGHT_NODEJS_PATH, node on PATH,
// then our own node download from nodejs.org (zero-dependency last mile).
func ensureNode(dir string) error {
	if hasBundledNode(dir) {
		// bundled node wins; clear any stale override that would beat it.
		return nil
	}
	if p := os.Getenv("PLAYWRIGHT_NODEJS_PATH"); p != "" {
		return nil
	}
	bin := "node"
	if runtime.GOOS == "windows" {
		bin = "node.exe"
	}
	if p, err := execLookPath(bin); err == nil {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		return os.Setenv("PLAYWRIGHT_NODEJS_PATH", abs)
	}
	// Nothing on the machine: fetch our own so the client PC needs nothing.
	return fetchNode(dir)
}

// NodeSource reports how the driver's node runtime is provided, for logging.
func NodeSource(dir string) string {
	if hasBundledNode(dir) {
		return "bundled"
	}
	if p := os.Getenv("PLAYWRIGHT_NODEJS_PATH"); p != "" {
		return "system:" + p
	}
	return "none"
}

// verifyBundle sanity-checks an unzipped prebuilt asset: cli.js present and
// at least one stealth marker applied.
func verifyBundle(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "package", "cli.js")); err != nil {
		return fmt.Errorf("no cli.js: %w", err)
	}
	// Walk package/lib for any bundle file carrying a stealth marker.
	found := false
	_ = filepath.Walk(filepath.Join(dir, "package", "lib"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || found {
			return nil
		}
		if !strings.HasSuffix(path, ".js") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 50_000_000 {
			return nil
		}
		text := string(data)
		if strings.Contains(text, "--disable-blink-features=AutomationControlled") ||
			strings.Contains(text, "/^f[0-9a-f]{32}$/") ||
			strings.Contains(text, "--headless=new") {
			found = true
		}
		return nil
	})
	if !found {
		return fmt.Errorf("no stealth markers in prebuilt bundle")
	}
	return nil
}

// releaseAssetName is the per-platform prebuilt bundle asset name, e.g.
// patchright-driver-1.62.0-win32_x64.zip.
func releaseAssetName() string {
	return fmt.Sprintf("patchright-driver-%s-%s.zip", strings.TrimPrefix(Version, "v"), platform())
}

// downloadReleaseAsset fetches the prebuilt patched bundle for this version
// from the patchright-go GitHub release. Override repo via
// PATCHRIGHT_GO_REPO (default notlousybook/patchright-go), tag via
// PATCHRIGHT_GO_TAG (default v + module version below).
func downloadReleaseAsset() ([]byte, error) {
	repo := os.Getenv("PATCHRIGHT_GO_REPO")
	if repo == "" {
		repo = "notlousybook/patchright-go"
	}
	tag := os.Getenv("PATCHRIGHT_GO_TAG")
	if tag == "" {
		tag = "v" + ModuleVersion
	}
	u := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repo, tag, releaseAssetName())
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", u, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func driverURLs() []string {
	plat := platform()
	ver := strings.TrimPrefix(Version, "v")
	var out []string
	if host := os.Getenv("PLAYWRIGHT_DOWNLOAD_HOST"); host != "" {
		out = append(out, fmt.Sprintf("%s/builds/driver/playwright-%s-%s.zip", host, ver, plat))
		return out
	}
	for _, m := range mirrors {
		out = append(out, fmt.Sprintf("%s/builds/driver/playwright-%s-%s.zip", m, ver, plat))
	}
	return out
}

func downloadDriver() ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	var last error
	for _, u := range driverURLs() {
		resp, err := client.Get(u)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			last = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("%s: %s", u, resp.Status)
			continue
		}
		if len(body) < 1024 {
			last = fmt.Errorf("%s: suspiciously small (%d bytes)", u, len(body))
			continue
		}
		return body, nil
	}
	if last == nil {
		last = fmt.Errorf("no driver URLs")
	}
	return nil, fmt.Errorf("could not download stock driver: %w", last)
}

func unzip(body []byte, dir string) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	for _, zf := range zr.File {
		target := filepath.Join(dir, zf.Name)
		// zip slip guard.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dir)+string(os.PathSeparator)) && filepath.Clean(target) != filepath.Clean(dir) {
			return fmt.Errorf("zip slip: %q", zf.Name)
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, zf.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
		if zf.Mode().Perm()&0o100 != 0 && runtime.GOOS != "windows" {
			if fi, err := os.Stat(target); err == nil {
				os.Chmod(target, fi.Mode()|0o111)
			}
		}
	}
	return nil
}
