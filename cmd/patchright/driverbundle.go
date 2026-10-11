package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/notlousybook/patchright-go/driver"
)

// cmdDriverBundle provisions a patched driver dir (stock download + bundle
// transforms, same as driver.Ensure) and rezips it as a release asset:
// patchright-driver-<version>-<platform>.zip. Used by CI so users can skip
// the transform step; driver.Ensure prefers the asset when present.
func cmdDriverBundle(args []string) {
	fs := flag.NewFlagSet("driver-bundle", flag.ExitOnError)
	out := fs.String("o", "", "output zip path (required)")
	dir := fs.String("dir", "", "already-provisioned driver dir (default: provision fresh)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "driver-bundle: -o output zip is required")
		os.Exit(1)
	}
	src := *dir
	if src == "" {
		// Provision fresh into a temp dir via the driver package.
		tmp, err := os.MkdirTemp("", "patchright-driver-*")
		if err != nil {
			fmt.Fprintln(os.Stderr, "driver-bundle:", err)
			os.Exit(1)
		}
		defer os.RemoveAll(tmp)
		fmt.Println("provisioning patched driver (stock download + bundle transforms)...")
		if err := driver.ProvisionTo(tmp); err != nil {
			fmt.Fprintln(os.Stderr, "driver-bundle:", err)
			os.Exit(1)
		}
		src = tmp
	}
	if err := zipDir(src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "driver-bundle:", err)
		os.Exit(1)
	}
	fmt.Println("wrote", *out)
}

func zipDir(src, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	defer zw.Close()
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		// Skip our stamp: the consumer re-stamps after verifying.
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(rel, ".patchright-stamp") {
			return nil
		}
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		_, err = io.Copy(w, r)
		return err
	})
}
