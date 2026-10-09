package impact

import (
	"strings"
	"testing"

	"github.com/notlousybook/patchright-go/patcher"
)

func TestExtractSymbols(t *testing.T) {
	syms := ExtractSymbols()
	if len(syms) == 0 {
		t.Fatal("no symbols")
	}
	for _, s := range syms {
		if s.Symbol == "" || s.PlaywrightFile == "" || s.PatchFile == "" {
			t.Errorf("incomplete record: %+v", s)
		}
	}
	// Dedup check.
	seen := map[string]bool{}
	for _, s := range syms {
		key := s.Symbol + "::" + string(s.Kind) + "::" + s.PlaywrightFile + "::" + s.PatchFile
		if seen[key] {
			t.Errorf("duplicate: %s", key)
		}
		seen[key] = true
	}
	// Protocol symbols present.
	found := false
	for _, s := range syms {
		if s.Kind == KindProtocolParam && strings.Contains(s.Symbol, "isolatedContext") {
			found = true
		}
	}
	if !found {
		t.Error("protocol symbols missing")
	}
}

func TestSymbolsCoverPatches(t *testing.T) {
	if missing := SymbolsCoverPatches(); len(missing) != 0 {
		t.Errorf("patches without symbols: %v", missing)
	}
	// Every symbol's patch must exist in the registry.
	names := map[string]bool{}
	for _, p := range patcher.Registry {
		names[p.Name] = true
	}
	for _, s := range SymbolSpecs {
		if !names[s.patchName] {
			t.Errorf("symbol references unknown patch %q", s.patchName)
		}
	}
}

func TestParsePatch(t *testing.T) {
	p := ParsePatch("@@ -1,3 +1,4 @@\n a\n-b\n+c\n+d\n e\n")
	if len(p.Hunks) != 1 || len(p.Additions) != 2 || len(p.Deletions) != 1 {
		t.Errorf("bad parse: %+v", p)
	}
	snip := DiffSnippetForAnchor(p, strPtr("R"), intPtr(2), 3)
	if !strings.Contains(snip, "+c") {
		t.Errorf("bad snippet:\n%s", snip)
	}
}

func TestLineDiff(t *testing.T) {
	d := LineDiff([]string{"a", "b", "c"}, []string{"a", "x", "c"})
	joined := strings.Join(d, "\n")
	if !strings.Contains(joined, "-b") || !strings.Contains(joined, "+x") {
		t.Errorf("bad diff:\n%s", joined)
	}
}

func TestClassify(t *testing.T) {
	if got := ClassifySymbol(true, false, "modified", nil, nil, true, KindMethod); got != ChangeSymbolRemoved {
		t.Errorf("removed: %s", got)
	}
	if got := ClassifySymbol(false, true, "modified", nil, nil, true, KindMethod); got != ChangeSymbolAdded {
		t.Errorf("added: %s", got)
	}
	if got := ClassifySymbol(true, true, "modified", []string{"f(a)"}, []string{"f(a, b?)"}, true, KindMethod); got != ChangeSignatureChanged {
		t.Errorf("sig: %s", got)
	}
	if got := ClassifySymbol(true, true, "modified", []string{"f(a)"}, []string{"f(a)"}, true, KindMethod); got != ChangeBodyChanged {
		t.Errorf("body: %s", got)
	}
	if got := ClassifySymbol(true, true, "modified", []string{"f(a)"}, []string{"f(a)"}, false, KindMethod); got != ChangeUnchanged {
		t.Errorf("untouched: %s", got)
	}
}

func TestAnalyze(t *testing.T) {
	oldText := "export class Foo {\n  bar() {\n    return 1;\n  }\n}\n"
	newText := "export class Foo {\n  bar() {\n    return 2;\n  }\n}\n"
	syms := []Record{{Symbol: "bar", Kind: KindMethod, PlaywrightFile: "packages/playwright-core/src/server/foo.ts", PatchFile: "patchFoo"}}
	files := []CompareFile{{
		Filename: "packages/playwright-core/src/server/foo.ts",
		Status:   "modified",
		Patch:    "@@ -1,5 +1,5 @@\n export class Foo {\n   bar() {\n-    return 1;\n+    return 2;\n   }\n }\n",
	}}
	fetch := func(tag, path string) (string, bool) {
		if strings.Contains(tag, "1") {
			return oldText, true
		}
		return newText, true
	}
	report, summary, diff, issue, err := Analyze("v1.59.0", "v1.60.0", "abc123", syms, files, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 1 || report.Summary.Affected != 1 {
		t.Errorf("bad summary: %+v", report.Summary)
	}
	if !strings.Contains(summary, "Patch Impact Report") || !strings.Contains(summary, "bar") {
		t.Errorf("bad summary markdown")
	}
	if !strings.Contains(diff, "return 2;") {
		t.Errorf("bad diff:\n%s", diff)
	}
	if !strings.Contains(issue, "1 affected") {
		t.Errorf("bad issue:\n%s", issue)
	}
	// Protocol classification.
	oldProto := map[string]any{"Frame": map[string]any{}}
	newProto := map[string]any{"Frame": map[string]any{"commands": 1}}
	if got := ProtocolChangeType("Frame.commands.x", oldProto, newProto); got != ChangeUnchanged {
		t.Errorf("proto: %s", got)
	}
}
