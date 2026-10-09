package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file ports patchright-nodejs/patchright_rebranding.ts (directory
// renames, package.json rewrites, README copies, module-specifier rewrites)
// and the patch_file_updater.yml doc-patch generation.

// rebrandPackages performs the on-disk package rename inside a patched
// Playwright checkout, mirroring patchRebranding().
func rebrandPackages(playwrightDir string) error {
	packagesDir := filepath.Join(playwrightDir, "packages")
	// Module-specifier rewrites happen before the rename (paths still old).
	rewriteDir := filepath.Join(packagesDir, "playwright")
	if st, err := os.Stat(rewriteDir); err == nil && st.IsDir() {
		if err := renameImportsAndExports(rewriteDir); err != nil {
			return err
		}
	}
	rename := func(old, new string) error {
		oldP, newP := filepath.Join(packagesDir, old), filepath.Join(packagesDir, new)
		if _, err := os.Stat(oldP); err != nil {
			return nil
		}
		if _, err := os.Stat(newP); err == nil {
			os.RemoveAll(newP)
		}
		return os.Rename(oldP, newP)
	}
	if err := rename("playwright-core", "patchright-core"); err != nil {
		return err
	}
	if err := rename("playwright", "patchright"); err != nil {
		return err
	}
	// READMEs.
	if data, err := os.ReadFile(filepath.Join(playwrightDir, "..", "README.md")); err == nil {
		os.WriteFile(filepath.Join(packagesDir, "patchright", "README.md"), data, 0o644)
	}
	os.WriteFile(filepath.Join(packagesDir, "patchright-core", "README.md"),
		[]byte("# patchright-core\n\nThis package contains the no-browser flavor of [Patchright](https://github.com/Kaliiiiiiiiii-Vinyzu/patchright)."), 0o644)
	// package.json rewrites.
	release := strings.TrimSpace(os.Getenv("patchright_release"))
	patchPackageJSON(filepath.Join(packagesDir, "patchright-core", "package.json"), "patchright-core", release, map[string]string{"patchright-core": "cli.js"}, nil)
	patchPackageJSON(filepath.Join(packagesDir, "patchright", "package.json"), "patchright", release, map[string]string{"patchright": "cli.js"}, map[string]string{"patchright-core": ""})
	return nil
}

func patchPackageJSON(path, name, release string, bin, deps map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return
	}
	pkg["name"] = name
	if release != "" {
		pkg["version"] = release
	}
	if author, ok := pkg["author"].(map[string]any); ok {
		author["name"] = "Microsoft Corportation, patched by github.com/Kaliiiiiiiiii-Vinyzu/"
	}
	pkg["homepage"] = "https://github.com/Kaliiiiiiiiii-Vinyzu/patchright"
	if repo, ok := pkg["repository"].(map[string]any); ok {
		repo["url"] = "https://github.com/Kaliiiiiiiiii-Vinyzu/patchright"
	}
	if bin != nil {
		pkg["bin"] = bin
	}
	if deps != nil {
		version, _ := pkg["version"].(string)
		resolved := map[string]string{}
		for k := range deps {
			resolved[k] = version
		}
		pkg["dependencies"] = resolved
	}
	out, err := json.MarshalIndent(pkg, "", "    ")
	if err != nil {
		return
	}
	os.WriteFile(path, append(out, '\n'), 0o644)
	fmt.Println("JSON file has been updated successfully.")
}

// renameImportsAndExports mirrors renameImportsAndExportsInDirectory via
// textual module-specifier rewrites (import/export/require/dynamic import).
func renameImportsAndExports(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		switch filepath.Ext(path) {
		case ".ts", ".js", ".mjs":
		default:
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		updated := rewriteSpecifier(text, "playwright-core", "patchright-core")
		updated = rewriteBarePlaywright(updated)
		if updated != text {
			fmt.Printf("Modified imports/exports in: %s\n", path)
			return os.WriteFile(path, []byte(updated), 0o644)
		}
		return nil
	})
}

func rewriteSpecifier(text, old, new string) string {
	// Quoted module specifiers containing old.
	var sb strings.Builder
	i := 0
	for i < len(text) {
		c := text[i]
		if c == '\'' || c == '"' || c == '`' {
			j := i + 1
			for j < len(text) && text[j] != c {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(text) {
				j++
			}
			lit := text[i:j]
			if strings.Contains(lit, old) {
				lit = strings.ReplaceAll(lit, old, new)
			}
			sb.WriteString(lit)
			i = j
			continue
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String()
}

func rewriteBarePlaywright(text string) string {
	// require()/import() args mentioning bare "playwright" (not -core, which
	// was already handled). Conservative: only inside quoted literals that
	// contain "playwright" but not "playwright-core" or "patchright".
	var sb strings.Builder
	i := 0
	for i < len(text) {
		c := text[i]
		if c == '\'' || c == '"' {
			j := i + 1
			for j < len(text) && text[j] != c {
				if text[j] == '\\' {
					j++
				}
				j++
			}
			if j < len(text) {
				j++
			}
			lit := text[i:j]
			if strings.Contains(lit, "playwright") && !strings.Contains(lit, "playwright-core") && !strings.Contains(lit, "patchright") {
				lit = strings.ReplaceAll(lit, "playwright", "patchright")
			}
			sb.WriteString(lit)
			i = j
			continue
		}
		sb.WriteByte(c)
		i++
	}
	return sb.String()
}

func cmdRebrand(args []string) {
	fs := flag.NewFlagSet("rebrand", flag.ExitOnError)
	dir := fs.String("playwright-dir", "playwright", "Patched Playwright checkout directory")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if err := rebrandPackages(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "rebrand:", err)
		os.Exit(1)
	}
}

// cmdDiffPatch ports patch_file_updater.yml's doc-patch generation: it
// applies all patches to a pristine copy and writes a unified-diff-style
// patchright.patch (documentation only).
func cmdDiffPatch(args []string) {
	fs := flag.NewFlagSet("diff-patch", flag.ExitOnError)
	dir := fs.String("playwright-dir", "playwright", "Pristine Playwright checkout directory")
	output := fs.String("output", "patchright.patch", "output patch file")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if err := writeDocPatch(*dir, *output); err != nil {
		fmt.Fprintln(os.Stderr, "diff-patch:", err)
		os.Exit(1)
	}
}
