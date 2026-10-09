package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// This file ports utils/release_version_check.sh: it compares the latest
// microsoft/playwright release tag against this repo's releases and reports
// whether a rebuild is needed.

type release struct {
	TagName string `json:"tag_name"`
}

func githubJSON(url string, v any) error {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github api %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func latestRelease(repo string) (string, error) {
	var r release
	if err := githubJSON("https://api.github.com/repos/"+repo+"/releases/latest", &r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("no releases for %s", repo)
	}
	return r.TagName, nil
}

// latestPlaywrightTag resolves the newest microsoft/playwright release tag.
// used by `patch` when --version is left at latest.
func latestPlaywrightTag() (string, error) {
	return latestRelease("microsoft/playwright")
}

func tagExists(repo, tag string) (bool, error) {
	var releases []release
	if err := githubJSON("https://api.github.com/repos/"+repo+"/releases?per_page=100", &releases); err != nil {
		return false, err
	}
	want := strings.TrimPrefix(tag, "v")
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == want {
			return true, nil
		}
	}
	return false, nil
}

func cmdVersionCheck(args []string) {
	fs := flag.NewFlagSet("version-check", flag.ExitOnError)
	repo := fs.String("repo", os.Getenv("REPO"), "Patchright repo OWNER/NAME")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *repo == "" {
		fmt.Fprintln(os.Stderr, "version-check: --repo or REPO must be set")
		os.Exit(1)
	}
	playwrightVersion, err := latestRelease("microsoft/playwright")
	if err != nil {
		fmt.Fprintln(os.Stderr, "playwright version:", err)
		os.Exit(1)
	}
	fmt.Println("Latest release of the Playwright Driver:", playwrightVersion)
	exists, err := tagExists(*repo, playwrightVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, "release list:", err)
		os.Exit(1)
	}
	if exists {
		fmt.Printf("%s is up to date with microsoft/playwright.\n", *repo)
		fmt.Println("proceed=false")
	} else {
		fmt.Printf("%s is behind microsoft/playwright. Building & Patching...\n", *repo)
		fmt.Println("proceed=true")
	}
	fmt.Println("playwright_version=" + playwrightVersion)
}
