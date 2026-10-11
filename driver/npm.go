package driver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This file implements the FULL stealth path: instead of transforming the
// stock bundle ourselves (bundle.go), we overlay the prebuilt patched
// `package/` tree from the patchright-core npm package (which is Vinyzu's CI
// output of the full 47-patch source pipeline) onto a stock driver zip.
//
// Why npm: patchright's CI publishes the patched playwright-core as
// patchright-core on npm (package/cli.js + package/lib/** — exactly the driver
// zip layout). Overlaying it onto the stock zip (which carries the bundled
// node binary we can't build) gives us a full-stealth patched driver with no
// npm runtime, no playwright source build, pure downloads.
//
// Fallback chain in provision():
//  1. our own release asset (CI-built, fully patched)
//  2. npm overlay (patchright-core)          <- full stealth
//  3. stock + in-place bundle transforms     <- partial stealth, tested

// npmRegistry is the patchright-core source. Override for testing.
var npmRegistry = "https://registry.npmjs.org"

// PatchrightCoreVersion maps our driver Version to the matching upstream
// patchright-core tag. Empty means "same as Version" (their default policy).
// Upstream skips some versions (e.g. no 1.62.0), so this may need pinning.
var PatchrightCoreVersion = ""

// patchrightCoreVersion resolves the npm tag to use.
func patchrightCoreVersion() string {
	if v := os.Getenv("PATCHRIGHT_CORE_VERSION"); v != "" {
		return v
	}
	if PatchrightCoreVersion != "" {
		return PatchrightCoreVersion
	}
	return strings.TrimPrefix(Version, "v")
}

type npmPackument struct {
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

// fetchPatchrightCoreTarball downloads and extracts the patchright-core
// `package/` tree into dest (a laid-out package dir, NOT the driver root).
func fetchPatchrightCoreTarball(dest string) error {
	pack, err := fetchPackument()
	if err != nil {
		return err
	}
	ver := patchrightCoreVersion()
	entry, ok := pack.Versions[ver]
	if !ok {
		return fmt.Errorf("patchright-core %s not published (available: check npm)", ver)
	}
	body, err := httpGet(entry.Dist.Tarball, 5*time.Minute)
	if err != nil {
		return err
	}
	if len(body) < 10_000 {
		return fmt.Errorf("patchright-core %s tarball suspiciously small (%d bytes)", ver, len(body))
	}
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("gunzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	files := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("untar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		// Everything in these tarballs lives under package/.
		if !strings.HasPrefix(name, "package/") {
			continue
		}
		rel := strings.TrimPrefix(name, "package/")
		if rel == "" {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if !within(dest, target) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
		// tarball modes are mostly 644; keep 755 for the tools that need it.
		os.Chmod(target, os.FileMode(hdr.Mode)&0o777|0o0200)
		files++
	}
	if files == 0 {
		return fmt.Errorf("patchright-core tarball had no package/ files")
	}
	return nil
}

func fetchPackument() (*npmPackument, error) {
	body, err := httpGet(npmRegistry+"/patchright-core", 30*time.Second)
	if err != nil {
		return nil, err
	}
	var p npmPackument
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if len(p.Versions) == 0 {
		return nil, fmt.Errorf("empty packument")
	}
	return &p, nil
}

// overlayPatchrightCore provisions a FULL stealth driver: stock zip for the
// node binary + skeleton, patchright-core's package tree on top.
func overlayPatchrightCore(dir string) error {
	if err := unzipMust(dir, downloadDriver); err != nil {
		return err
	}
	pkgDir := filepath.Join(dir, "package")
	if err := fetchPatchrightCoreTarball(pkgDir); err != nil {
		return err
	}
	return verifyBundle(dir)
}

// provisionNpmOnly builds a driver dir from the patchright-core tarball alone,
// with no CDN involved. Used when Microsoft's driver CDN is down; the caller
// must supply node via PLAYWRIGHT_NODEJS_PATH or PATH.
func provisionNpmOnly(dir string) error {
	return fetchPatchrightCoreTarball(filepath.Join(dir, "package"))
}

// unzipMust downloads a body and unzips it into dir (used once per strategy).
func unzipMust(dir string, download func() ([]byte, error)) error {
	body, err := download()
	if err != nil {
		return err
	}
	return unzip(body, dir)
}

// within guards path traversal.
func within(root, target string) bool {
	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(target)
	if rootClean == targetClean {
		return true
	}
	return strings.HasPrefix(targetClean, rootClean+string(filepath.Separator))
}

func httpGet(url string, timeout time.Duration) ([]byte, error) {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// zipReaderFor exposes the zip reader for tests.
func zipReaderFor(body []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(body), int64(len(body)))
}
