package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/notlousybook/patchright-go/impact"
	"github.com/notlousybook/patchright-go/testmods"
)

// cmdImpactCheck ports utils/check_patch_impact.ts.
func cmdImpactCheck(args []string) {
	fs := flag.NewFlagSet("impact-check", flag.ExitOnError)
	oldVersion := fs.String("old-version", "", "old Playwright version (required)")
	newVersion := fs.String("new-version", "", "new Playwright version (required)")
	reportPath := fs.String("report", "report.json", "report JSON path")
	summaryPath := fs.String("summary", "step_summary.md", "summary markdown path")
	diffPath := fs.String("diff", "affected_diff.patch", "affected diff path")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *oldVersion == "" || *newVersion == "" {
		fmt.Fprintln(os.Stderr, "impact-check: --old-version and --new-version are required")
		os.Exit(1)
	}
	oldTag := withV(*oldVersion)
	newTag := withV(*newVersion)
	sha := os.Getenv("GITHUB_SHA")
	if sha == "" {
		sha = "main"
	}
	client := &impact.Client{Token: os.Getenv("GITHUB_TOKEN")}
	if token := os.Getenv("GH_TOKEN"); client.Token == "" && token != "" {
		client.Token = token
	}
	files, err := client.Compare(oldTag, newTag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare:", err)
		os.Exit(1)
	}
	fetch := func(tag, path string) (string, bool) {
		text, ok, err := client.RawFile(tag, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fetch %s at %s: %v\n", path, tag, err)
			return "", false
		}
		return text, ok
	}
	report, summary, diff, issue, err := impact.Analyze(oldTag, newTag, sha, impact.ExtractSymbols(), files, fetch)
	if err != nil {
		fmt.Fprintln(os.Stderr, "analyze:", err)
		os.Exit(1)
	}
	writeFile := func(path, text string) {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write:", err)
			os.Exit(1)
		}
	}
	raw, _ := json.MarshalIndent(report, "", "  ")
	writeFile(*reportPath, string(raw)+"\n")
	writeFile(*summaryPath, summary)
	writeFile(*diffPath, diff)
	writeFile("issue_body.md", issue)
	fmt.Printf("Analyzed %d patched symbols (%d affected).\n", report.Summary.Total, report.Summary.Affected)
}

func withV(v string) string {
	if len(v) > 0 && v[0] == 'v' {
		return v
	}
	return "v" + v
}

// cmdModifyTests ports utils/modify_tests.ts.
func cmdModifyTests(args []string) {
	fs := flag.NewFlagSet("modify-tests", flag.ExitOnError)
	dir := fs.String("playwright-dir", "playwright", "Playwright checkout directory")
	customTests := fs.String("custom-tests", "utils/custom_tests", "custom tests directory")
	dryRun := fs.Bool("dry-run", false, "report without writing (MODIFY_TESTS_DRY_RUN=1 also works)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if os.Getenv("MODIFY_TESTS_DRY_RUN") == "1" {
		*dryRun = true
	}
	if _, err := os.Stat(*dir); err != nil {
		fmt.Fprintln(os.Stderr, "playwright dir:", err)
		os.Exit(1)
	}
	if _, err := os.Stat(*customTests); err != nil {
		fmt.Fprintln(os.Stderr, "custom tests dir:", err)
		os.Exit(1)
	}
	report, err := testmods.ModifyTree(*dir, *customTests, *dryRun)
	if err != nil {
		fmt.Fprintln(os.Stderr, "modify-tests:", err)
		os.Exit(1)
	}
	mode := "write"
	if *dryRun {
		mode = "dry-run"
	}
	fmt.Printf("[modify_tests] mode=%s\n", mode)
	fmt.Print(report.String())
}
