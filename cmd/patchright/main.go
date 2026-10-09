// Command patchright does everything: clone a playwright checkout, patch it,
// check versions, analyze patch impact, rewrite tests, rebrand packages, all
// of it. `go run ./cmd/patchright --help` for the full list, or just `patch`
// and go touch grass while npm builds.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"

	pr "github.com/notlousybook/patchright-go/patcher"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "patch":
		cmdPatch(os.Args[2:])
	case "list":
		cmdList()
	case "version-check":
		cmdVersionCheck(os.Args[2:])
	case "symbols":
		cmdSymbols(os.Args[2:])
	case "impact-check":
		cmdImpactCheck(os.Args[2:])
	case "modify-tests":
		cmdModifyTests(os.Args[2:])
	case "rebrand":
		cmdRebrand(os.Args[2:])
	case "diff-patch":
		cmdDiffPatch(os.Args[2:])
	case "format":
		cmdFormat(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: patchright <command> [flags]")
	fmt.Fprintln(os.Stderr, "  patch --playwright-dir DIR [--version TAG] [--only a,b]  clone + patch a Playwright checkout")
	fmt.Fprintln(os.Stderr, "  list                                                   list registered patches")
	fmt.Fprintln(os.Stderr, "  version-check --repo OWNER/REPO                        compare latest Playwright vs Patchright releases")
	fmt.Fprintln(os.Stderr, "  symbols [--new-version TAG] [-o FILE]                   extract patched symbols (extract_patched_symbols.ts)")
	fmt.Fprintln(os.Stderr, "  impact-check --old-version V --new-version V [--report F --summary F --diff F]  analyze patch impact (check_patch_impact.ts)")
	fmt.Fprintln(os.Stderr, "  modify-tests --playwright-dir DIR --custom-tests DIR [--dry-run]  rewrite upstream tests (modify_tests.ts)")
	fmt.Fprintln(os.Stderr, "  rebrand --playwright-dir DIR                             rename packages to patchright* (patchright_rebranding.ts)")
	fmt.Fprintln(os.Stderr, "  diff-patch --playwright-dir DIR [--output F]              write documentation patchright.patch (patch_file_updater.yml)")
	fmt.Fprintln(os.Stderr, "  format [--check]                                        gofmt the repo (format-indentation.ts)")
}

func cmdList() {
	for _, p := range pr.Registry {
		fmt.Printf("%-32s %s\n", p.Name, p.Target)
	}
}

func 	cmdPatch(args []string) {
	fs := flag.NewFlagSet("patch", flag.ExitOnError)
	dir := fs.String("playwright-dir", "playwright", "Playwright checkout directory")
	version := fs.String("version", "latest", "Playwright tag to clone (default: latest release, 'latest' also works explicitly)")
	only := fs.String("only", "", "comma-separated patch names (default: all)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	tag := *version
	if tag == "" || tag == "latest" {
		resolved, err := latestPlaywrightTag()
		if err != nil {
			fmt.Fprintf(os.Stderr, "couldn't resolve latest (%v), falling back to %s\n", err, pr.DefaultPlaywrightVersion)
			tag = pr.DefaultPlaywrightVersion
		} else {
			tag = resolved
		}
	}
	if _, err := os.Stat(*dir); os.IsNotExist(err) {
		fmt.Printf("cloning %s %s into %s\n", pr.PlaywrightRepo, tag, *dir)
		clone := exec.Command("git", "clone", "--branch", tag, "--depth", "1", pr.PlaywrightRepo, *dir)
		clone.Stdout = os.Stdout
		clone.Stderr = os.Stderr
		if err := clone.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "clone:", err)
			os.Exit(1)
		}
	}
	fset, err := pr.Open(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	if *only == "" {
		err = pr.ApplyAll(fset)
	} else {
		err = pr.ApplyNames(fset, splitCSV(*only)...)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "patch:", err)
		os.Exit(1)
	}
	if err := fset.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "save:", err)
		os.Exit(1)
	}
	fmt.Printf("patched %d files\n", len(fset.Dirty()))
	for _, f := range fset.Dirty() {
		fmt.Println("  " + f)
	}
}

func cmdSymbols(args []string) {
	fs := flag.NewFlagSet("symbols", flag.ExitOnError)
	output := fs.String("o", "", "output file (default: stdout)")
	outputLong := fs.String("output", "", "output file (default: stdout)")
	newVersion := fs.String("new-version", "", "upstream tag for line resolution (informational)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	_ = newVersion
	outPath := *output
	if outPath == "" {
		outPath = *outputLong
	}
	var enc *json.Encoder
	if outPath == "" {
		enc = json.NewEncoder(os.Stdout)
	} else {
		f, err := os.Create(outPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "create:", err)
			os.Exit(1)
		}
		defer f.Close()
		enc = json.NewEncoder(f)
	}
	enc.SetIndent("", "  ")
	manifest, err := pr.Manifest()
	if err != nil {
		fmt.Fprintln(os.Stderr, "manifest:", err)
		os.Exit(1)
	}
	type symbol struct {
		PatchFunc      string   `json:"patchFunc"`
		Kind           string   `json:"kind"`
		ContextSymbols []string `json:"contextSymbols"`
		BodyFile       string   `json:"bodyFile"`
	}
	var syms []symbol
	for _, e := range manifest {
		syms = append(syms, symbol{e.PatchFunc, e.CallKind, e.ContextSymbols, e.BodyFile})
	}
	for _, p := range pr.Registry {
		syms = append(syms, symbol{p.Name, "patch", nil, p.Target})
	}
	if err := enc.Encode(syms); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}

func splitCSV(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			part := s[start:i]
			if part != "" {
				out = append(out, part)
			}
			start = i + 1
		}
	}
	return out
}
