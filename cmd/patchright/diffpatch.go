package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pr "github.com/notlousybook/patchright-go/patcher"
)

// writeDocPatch mirrors the patch_file_updater.yml doc generation: it opens
// the checkout, applies every patch in memory, and writes a unified diff of
// old vs new per file (excluding protocol.yml, like the workflow's -x flag).
func writeDocPatch(dir, output string) error {
	fs, err := pr.Open(dir)
	if err != nil {
		return err
	}
	// Snapshot originals.
	originals := map[string]string{}
	for _, p := range pr.Registry {
		_ = p
	}
	// Collect pre-patch contents lazily: snapshot every loaded file.
	for _, rel := range fs.Files() {
		if strings.HasSuffix(rel, "protocol.yml") {
			continue
		}
		text, err := fs.Get(rel)
		if err != nil {
			continue
		}
		originals[rel] = text
	}
	if err := pr.ApplyAll(fs); err != nil {
		return err
	}
	var sb strings.Builder
	sb.WriteString("# NOTE: This patch file is generated automatically and is not used, it is only for documentation. The driver is actually patched using the Go patcher (patcher/), see [.github/workflows/patch_file_updater.yml](https://github.com/notlousybook/patchright-go/blob/main/.github/workflows/patch_file_updater.yml)\n")
	for _, rel := range fs.Dirty() {
		if strings.HasSuffix(rel, "protocol.yml") {
			continue
		}
		oldText := originals[rel]
		newText, err := fs.Get(rel)
		if err != nil {
			continue
		}
		if oldText == newText {
			continue
		}
		sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", rel, rel, rel, rel))
		sb.WriteString(unifiedDiff(oldText, newText))
	}
	return os.WriteFile(output, []byte(sb.String()), 0o644)
}

// unifiedDiff renders a minimal unified diff with 3 lines of context using an
// LCS over lines (good enough for documentation output).
func unifiedDiff(oldText, newText string) string {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	m, n := len(oldLines), len(newLines)
	// Cap work for huge files.
	if m*n > 4_000_000 {
		return "@@ binary or too large @@\n"
	}
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	type op struct {
		kind string
		text string
	}
	var ops []op
	i, j := 0, 0
	for i < m && j < n {
		if oldLines[i] == newLines[j] {
			ops = append(ops, op{" ", oldLines[i]})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, op{"-", oldLines[i]})
			i++
		} else {
			ops = append(ops, op{"+", newLines[j]})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, op{"-", oldLines[i]})
	}
	for ; j < n; j++ {
		ops = append(ops, op{"+", newLines[j]})
	}
	// Emit hunks with 3 lines of context.
	var sb strings.Builder
	hunk := func(start, end int) {
		// Compute old/new ranges.
		om, nm := 0, 0
		for k := start; k < end; k++ {
			if ops[k].kind != "+" {
				om++
			}
			if ops[k].kind != "-" {
				nm++
			}
		}
		// Line numbers: count preceding non-added / non-removed.
		ol, nl := 1, 1
		for k := 0; k < start; k++ {
			if ops[k].kind != "+" {
				ol++
			}
			if ops[k].kind != "-" {
				nl++
			}
		}
		fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", ol, om, nl, nm)
		for k := start; k < end; k++ {
			sb.WriteString(ops[k].kind + ops[k].text + "\n")
		}
	}
	// Find change runs.
	k := 0
	for k < len(ops) {
		if ops[k].kind == " " {
			k++
			continue
		}
		start := k - 3
		if start < 0 {
			start = 0
		}
		end := k + 1
		for end < len(ops) && (ops[end].kind != " " || (end+1 < len(ops) && ops[end+1].kind != " ")) {
			end++
			if end-k > 200 {
				break
			}
		}
		// Include trailing context.
		trail := end + 3
		if trail > len(ops) {
			trail = len(ops)
		}
		// Only extend over context lines.
		for end < trail && ops[end].kind == " " {
			end++
		}
		hunk(start, end)
		k = end
	}
	_ = filepath.Separator
	return sb.String()
}
