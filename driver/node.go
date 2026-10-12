package driver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// This file closes the last external dependency: when no node is bundled
// (npm-only provisioning during a playwright CDN outage) and none is on PATH,
// we fetch our own from nodejs.org. Result: a zero-dependency patched driver
// on a machine that has neither playwright's driver CDN nor node installed.

// NodeVersion is the node runtime we self-provision. LTS line, matching what
// playwright's own driver bundles roughly (they pin to an LTS).
const NodeVersion = "v22.11.0"

// nodeDist is the official node distribution base. Override with
// PATCHRIGHT_NODEJS_DIST for mirrors/air-gapped setups.
var nodeDist = "https://nodejs.org/dist"

// nodePlatform maps Go's GOOS/GOARCH to node's archive names.
func nodePlatform() (name string, ext string) {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "arm64":
			return "win-arm64", "zip"
		default:
			return "win-x64", "zip"
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "darwin-arm64", "tar.gz"
		default:
			return "darwin-x64", "tar.gz"
		}
	default:
		switch runtime.GOARCH {
		case "arm64":
			return "linux-arm64", "tar.gz"
		default:
			return "linux-x64", "tar.gz"
		}
	}
}

// nodeDownloadURL is the archive we fetch for this platform.
func nodeDownloadURL() string {
	if mirror := os.Getenv("PATCHRIGHT_NODEJS_DIST"); mirror != "" {
		nodeDist = mirror
	}
	name, ext := nodePlatform()
	return fmt.Sprintf("%s/%s/node-%s-%s.%s", nodeDist, NodeVersion, NodeVersion, name, ext)
}

// nodeArchiveDir is the root directory inside the node archive.
func nodeArchiveDir() (string, string) {
	name, ext := nodePlatform()
	return fmt.Sprintf("node-%s-%s", NodeVersion, name), ext
}

// nodeBinaryPath is where node lives inside that archive.
func nodeBinaryPath() string {
	if runtime.GOOS == "windows" {
		return "/node.exe"
	}
	return "/bin/node"
}

// fetchNode downloads node and installs the bare binary into dir, executable.
// Only the runtime binary is kept; headers/npm/docs are dropped.
func fetchNode(dir string) error {
	base, ext := nodeArchiveDir()
	url := nodeDownloadURL()
	body, err := httpGet(url, 10*time.Minute)
	if err != nil {
		return fmt.Errorf("download node: %w", err)
	}
	if len(body) < 1_000_000 {
		return fmt.Errorf("node archive from %s suspiciously small (%d bytes)", url, len(body))
	}
	switch ext {
	case "zip":
		if err := extractNodeFromZip(body, base, dir); err != nil {
			return err
		}
	default:
		if err := extractNodeFromTarGz(body, base, dir); err != nil {
			return err
		}
	}
	target := filepath.Join(dir, nodeFileName())
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("node binary missing after extraction: %w", err)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(target); err == nil {
			if err := os.Chmod(target, fi.Mode()|0o111); err != nil {
				return err
			}
		}
	}
	return verifyNodeRuns(target)
}

// verifyNodeRuns does a cheap `--version` smoke so a corrupt or
// foreign-architecture download fails loudly instead of at first launch.
func verifyNodeRuns(bin string) error {
	cmd := exec.Command(bin, "--version")
	done := make(chan error, 1)
	var out []byte
	go func() {
		var err error
		out, err = cmd.Output()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("downloaded node does not run: %w", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		return fmt.Errorf("node --version timed out")
	}
	if !bytes.Contains(out, []byte("v")) {
		return fmt.Errorf("node --version output unexpected: %q", string(out))
	}
	return nil
}

func extractNodeFromZip(body []byte, archiveDir, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return err
	}
	want := archiveDir + nodeBinaryPath()
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(zf.Name))
		if name != filepath.ToSlash(filepath.Clean(want)) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(filepath.Join(dest, nodeFileName()))
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		return out.Close()
	}
	return fmt.Errorf("%s not found in node zip", want)
}

func extractNodeFromTarGz(body []byte, archiveDir, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	want := archiveDir + nodeBinaryPath()
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if name != filepath.ToSlash(filepath.Clean(want)) {
			continue
		}
		out, err := os.Create(filepath.Join(dest, nodeFileName()))
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("%s not found in node tarball", want)
}
