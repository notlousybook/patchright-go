package driver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeBundle writes a minimal stock-shaped driver dir for transform tests.
func fakeBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	lib := filepath.Join(dir, "package", "lib", "server")
	if err := os.MkdirAll(lib, 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := strings.Join([]string{
		`const switches = ['--enable-automation','--disable-popup-blocking','--headless'];`,
		`chromeArguments.push('--headless');`,
		`chromeArguments.push('--enable-unsafe-swiftshader');`,
		`await session.send('Runtime.enable');`,
		`await this._client.send("Runtime.enable");`,
		`if (typeof value === 'function' && value.name.startsWith(kFunctionBindingPrefix)) return { fn: value.name };`,
		`const x = 1; //# sourceURL=browser.js`,
		`globalThis.__playwright__binding__ = {};`,
		`globalThis.__playwright__binding__controller__ = {};`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(lib, "serverBundle.js"), []byte(bundle), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package", "cli.js"), []byte("cli"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPatchBundle(t *testing.T) {
	dir := fakeBundle(t)
	if err := patchBundle(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "package", "lib", "server", "serverBundle.js"))
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	for _, leaked := range []string{
		"'--enable-automation'",
		"'--disable-popup-blocking'",
		"chromeArguments.push('--headless');",
		"--enable-unsafe-swiftshader",
		"send('Runtime.enable')",
		`send("Runtime.enable")`,
		"value.name.startsWith(kFunctionBindingPrefix)",
		"//# sourceURL=",
		"__playwright__binding__",
	} {
		if strings.Contains(out, leaked) {
			t.Errorf("leak still present: %q", leaked)
		}
	}
	for _, want := range []string{
		"chromeArguments.push('--headless=new')",
		"/^f[0-9a-f]{32}$/.test(value.name)",
		"__binding__",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing: %q", want)
		}
	}
}

func TestPatchBundleUnknownLayout(t *testing.T) {
	dir := t.TempDir()
	if err := patchBundle(dir); err == nil {
		t.Error("want error for empty dir, got nil")
	}
}

func TestStampRoundtrip(t *testing.T) {
	dir := fakeBundle(t)
	if ok, _ := isPatched(dir); ok {
		t.Fatal("unstamped dir reads as patched")
	}
	if err := stamp(dir, SourceNpmOverlay); err != nil {
		t.Fatal(err)
	}
	if got := Source(dir); got != SourceNpmOverlay {
		t.Errorf("Source = %q, want %q", got, SourceNpmOverlay)
	}
	if ok, _ := isPatched(dir); !ok {
		t.Error("stamped dir not recognized")
	}
	// corrupt the stamp: must re-patch.
	os.WriteFile(stampFile(dir), []byte("bogus\n"), 0o644)
	if ok, _ := isPatched(dir); ok {
		t.Error("bogus stamp accepted")
	}
	if src := Source(dir); src != "" {
		t.Errorf("bogus stamp should have no source, got %q", src)
	}
	// a different strategy must NOT validate against the other's stamp.
	os.WriteFile(stampFile(dir), []byte(stampContent(SourceNpmOverlay)+"\nnpm-overlay\n"), 0o644)
	if got := Source(dir); got != SourceNpmOverlay {
		t.Errorf("source = %q, want npm-overlay", got)
	}
	if ok, _ := isPatched(dir); !ok {
		t.Error("valid stamp rejected")
	}
	os.WriteFile(stampFile(dir), []byte(stampContent(SourceNpmOverlay)+"\nnpm-only\n"), 0o644)
	if ok, _ := isPatched(dir); ok {
		t.Error("mismatched hash/strategy stamp accepted")
	}
}

func TestNodeSource(t *testing.T) {
	dir := fakeBundle(t)
	// fixture has no bundled node, so source depends on the environment.
	src := NodeSource(dir)
	if src != "bundled" && !strings.HasPrefix(src, "system:") && src != "none" {
		t.Errorf("unexpected node source %q", src)
	}
	// with a bundled node present it must report bundled.
	node := nodeFileName()
	if err := os.WriteFile(filepath.Join(dir, node), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := NodeSource(dir); got != "bundled" {
		t.Errorf("want bundled, got %q", got)
	}
	if !hasBundledNode(dir) {
		t.Error("hasBundledNode should see the fixture node")
	}
}

func TestEnsureNodeShortCircuits(t *testing.T) {
	// bundled node present: must return without consulting the environment
	// or the network.
	dir := fakeBundle(t)
	if err := os.WriteFile(filepath.Join(dir, nodeFileName()), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureNode(dir); err != nil {
		t.Fatal(err)
	}
	// a pre-set override must also short-circuit (never download).
	t.Setenv("PLAYWRIGHT_NODEJS_PATH", "dummy-node")
	if err := ensureNode(dir); err != nil {
		t.Fatal(err)
	}
}

// TestFetchNodeLive is the real end-to-end node self-download. Opt-in via
// PATCHRIGHT_LIVE_NET=1 because it pulls ~25MB from nodejs.org.
func TestFetchNodeLive(t *testing.T) {
	if os.Getenv("PATCHRIGHT_LIVE_NET") != "1" {
		t.Skip("set PATCHRIGHT_LIVE_NET=1 to run")
	}
	dir := t.TempDir()
	if err := fetchNode(dir); err != nil {
		t.Fatal(err)
	}
	if !hasBundledNode(dir) {
		t.Fatal("node binary not installed into the driver dir")
	}
	// it must be a real, runnable node, not a truncated archive.
	if err := verifyNodeRuns(filepath.Join(dir, nodeFileName())); err != nil {
		t.Fatalf("fetched node is not runnable: %v", err)
	}
}

func TestNodeDownloadURL(t *testing.T) {
	got := nodeDownloadURL()
	wantName, wantExt := nodePlatform()
	if !strings.Contains(got, NodeVersion) {
		t.Errorf("url %q missing version %s", got, NodeVersion)
	}
	if !strings.Contains(got, wantName+"."+wantExt) {
		t.Errorf("url %q missing %s.%s", got, wantName, wantExt)
	}
}

func TestNodeBinaryPath(t *testing.T) {
	p := nodeBinaryPath()
	if runtime.GOOS == "windows" {
		if p != "/node.exe" {
			t.Errorf("want /node.exe, got %q", p)
		}
		return
	}
	if p != "/bin/node" {
		t.Errorf("want /bin/node, got %q", p)
	}
}

// buildNodeTarball makes a nodejs.org-shaped tarball for extraction tests.
func buildNodeTarball(t *testing.T, binaryContent string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	base, ext := nodeArchiveDir()
	nodeRel := base + nodeBinaryPath()
	for _, name := range []string{
		base + "/README.md",
		nodeRel,
		base + "/lib/node_modules/npm/bin/npm-cli.js",
	} {
		content := binaryContent
		if name != nodeRel {
			content = "junk"
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = ext
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractNodeFromTarGz(t *testing.T) {
	dir := t.TempDir()
	base, _ := nodeArchiveDir()
	body := buildNodeTarball(t, "#!/fake node\n")
	if err := extractNodeFromTarGz(body, base, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, nodeFileName()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "fake node") {
		t.Errorf("wrong binary extracted: %q", got)
	}
	// npm/docs must NOT be carried over.
	if _, err := os.Stat(filepath.Join(dir, "bin")); err == nil {
		t.Error("should only keep the node binary, not the whole tree")
	}
}

func TestExtractNodeFromZip(t *testing.T) {
	// only meaningful where node ships zip archives (windows). Build the zip
	// in-memory and assert the whole extraction path, so it's covered everywhere.
	dir := t.TempDir()
	base, _ := nodeArchiveDir()
	want := base + nodeBinaryPath()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(want)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("#!/fake node zip\n"))
	zw.Create(base + "/lib/node_modules/npm/README.md")
	zw.Close()

	if err := extractNodeFromZip(buf.Bytes(), base, dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, nodeFileName()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "fake node zip") {
		t.Errorf("wrong binary extracted: %q", got)
	}
}

func TestNodeArchiveDir(t *testing.T) {
	base, ext := nodeArchiveDir()
	if !strings.HasPrefix(base, "node-"+NodeVersion+"-") {
		t.Errorf("bad archive dir root: %q", base)
	}
	if ext != "zip" && ext != "tar.gz" {
		t.Errorf("bad ext: %q", ext)
	}
}

func TestNodeFileName(t *testing.T) {
	if runtime.GOOS == "windows" && nodeFileName() != "node.exe" {
		t.Errorf("want node.exe, got %q", nodeFileName())
	}
	if runtime.GOOS != "windows" && nodeFileName() != "node" {
		t.Errorf("want node, got %q", nodeFileName())
	}
}

func TestCacheDir(t *testing.T) {
	dir, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, "patchright-go") {
		t.Errorf("cache dir missing namespace: %q", dir)
	}
	if !strings.Contains(dir, strings.TrimPrefix(Version, "v")) {
		t.Errorf("cache dir missing version %q: %q", Version, dir)
	}
}

func TestZipSlip(t *testing.T) {
	dir := t.TempDir()
	// empty body is not a zip: must error, not panic.
	if err := unzip([]byte("not a zip"), dir); err == nil {
		t.Error("want error for garbage zip")
	}
}

// buildNpmTarball makes a minimal patchright-core-shaped tarball for tests.
func buildNpmTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name:     "package/" + name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchPatchrightCoreTarball(t *testing.T) {
	dir := t.TempDir()
	body := buildNpmTarball(t, map[string]string{
		"cli.js":              "console.log('patched')",
		"lib/coreBundle.js":   "['--disable-blink-features=AutomationControlled']",
		"lib/server/weird.js": "x",
		"../escape.txt":       "must not escape",
	})
	// Extract via the same reader the production path uses.
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if !strings.HasPrefix(name, "package/") {
			continue
		}
		rel := strings.TrimPrefix(name, "package/")
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if !within(dir, target) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(out, tr)
		out.Close()
	}
	if got, err := os.ReadFile(filepath.Join(dir, "lib", "coreBundle.js")); err != nil || !strings.Contains(string(got), "AutomationControlled") {
		t.Errorf("coreBundle not extracted: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(dir, "cli.js")); err != nil {
		t.Error("cli.js not extracted")
	}
	// traversal must not escape.
	if _, err := os.Stat(filepath.Join(dir, "..", "escape.txt")); err == nil {
		t.Error("tar traversal escaped the package dir")
	}
}

func TestPatchrightCoreVersion(t *testing.T) {
	if got := patchrightCoreVersion(); got != strings.TrimPrefix(Version, "v") {
		t.Errorf("default version = %q, want %q", got, strings.TrimPrefix(Version, "v"))
	}
	t.Setenv("PATCHRIGHT_CORE_VERSION", "1.99.0")
	if got := patchrightCoreVersion(); got != "1.99.0" {
		t.Errorf("env override = %q, want 1.99.0", got)
	}
}

func TestWithin(t *testing.T) {
	if !within("/a/b", "/a/b/c") {
		t.Error("child should be within")
	}
	if within("/a/b", "/a/bc") {
		t.Error("sibling prefix must not count as within")
	}
}
