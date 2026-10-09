package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// cmdFormat ports utils/format-indentation.ts: where the TS repo enforces
// prettier conventions on patch sources (max passes, tab indentation), the Go
// port enforces gofmt across the module. --check exits nonzero when any file
// needs formatting (for CI).
func cmdFormat(args []string) {
	fs := flag.NewFlagSet("format", flag.ExitOnError)
	check := fs.Bool("check", false, "list unformatted files and exit nonzero")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "format:", err)
		os.Exit(1)
	}
	if *check {
		out, err := exec.Command("gofmt", "-l", root).Output()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gofmt:", err)
			os.Exit(1)
		}
		if len(out) != 0 {
			fmt.Printf("unformatted files:\n%s", out)
			os.Exit(1)
		}
		fmt.Println("all files formatted")
		return
	}
	// Format in place: gofmt -w over every directory with Go files.
	var dirs []string
	seen := map[string]bool{}
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".go" {
			dir := filepath.Dir(path)
			if !seen[dir] {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "format:", err)
		os.Exit(1)
	}
	for _, dir := range dirs {
		cmd := exec.Command("gofmt", "-w", dir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "gofmt:", err)
			os.Exit(1)
		}
	}
	fmt.Println("formatted")
}

func moduleRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", cwd)
		}
		dir = parent
	}
}
