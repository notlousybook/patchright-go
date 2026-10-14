package patcher

import (
	"strings"
	"testing"
)

// TestRemovedSwitchFlags pins the canonical flag set that stealth and
// driver/bundle.go derive from. If this changes, the bundle transforms and the
// launch filter change with it automatically — this test exists to make that
// contract visible rather than accidental.
func TestRemovedSwitchFlags(t *testing.T) {
	if len(RemovedSwitchFlags) != len(SwitchesToDisable) {
		t.Errorf("RemovedSwitchFlags has %d entries, want %d", len(RemovedSwitchFlags), len(SwitchesToDisable))
	}
	for _, f := range RemovedSwitchFlags {
		if !strings.HasPrefix(f, "--") {
			t.Errorf("flag %q is not a bare --flag", f)
		}
		if strings.Contains(f, "'") || strings.Contains(f, `"`) {
			t.Errorf("flag %q still carries quotes", f)
		}
	}
	// No duplicates: a dup would silently do nothing but confuse readers.
	seen := map[string]bool{}
	for _, f := range RemovedSwitchFlags {
		if seen[f] {
			t.Errorf("duplicate flag %q", f)
		}
		seen[f] = true
	}
}

func TestFilterChromiumSwitchesMatchesOwner(t *testing.T) {
	// Every flag the owner lists must be stripped by the filter, and the
	// caller's own args must survive otherwise.
	in := append([]string{}, RemovedSwitchFlags...)
	in = append(in, "--keep-me=1")
	out := FilterChromiumSwitches(in)
	for _, f := range RemovedSwitchFlags {
		for _, o := range out {
			if o == f {
				t.Errorf("owner flag %q survived the filter", f)
			}
		}
	}
	joined := strings.Join(out, " ")
	if !strings.Contains(joined, "--keep-me=1") {
		t.Errorf("unrelated arg dropped: %q", joined)
	}
	if !strings.Contains(joined, "--disable-blink-features=AutomationControlled") {
		t.Errorf("stealth switch not appended: %q", joined)
	}
}
