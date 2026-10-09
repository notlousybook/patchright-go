package testmods

import (
	"strings"
	"testing"
)

func TestEvaluateInsertion(t *testing.T) {
	in := "await page.evaluate(() => 1);\nawait page.evaluateHandle('x');\n"
	updated, cf := ModifyFile("tests/page/sample.spec.ts", in)
	if cf.IsolatedContextInsertions != 2 {
		t.Errorf("insertions = %d, want 2:\n%s", cf.IsolatedContextInsertions, updated)
	}
	if !strings.Contains(updated, "page.evaluate(() => 1, undefined, undefined, false)") {
		t.Errorf("evaluate not rewritten:\n%s", updated)
	}
	if !strings.Contains(updated, "page.evaluateHandle('x', undefined, undefined, false)") {
		t.Errorf("evaluateHandle not rewritten:\n%s", updated)
	}
}

func TestEvaluateSkips(t *testing.T) {
	// Identifier first arg, spread, already-full args: all skipped.
	in := "await page.evaluate(foo);\nawait page.evaluate(() => 1, ...rest);\nawait page.evaluate(() => 1, undefined, undefined, false);\n"
	_, cf := ModifyFile("tests/page/sample.spec.ts", in)
	if cf.IsolatedContextInsertions != 0 {
		t.Errorf("insertions = %d, want 0", cf.IsolatedContextInsertions)
	}
}

func TestNormalization(t *testing.T) {
	// 3-arg options-object form with isolatedContext:false normalizes to
	// (fn, undefined, false); mirrors normalizeIsolatedContextArgument.
	in := "await page.evaluate('x', { a: 1 }, { isolatedContext: false });\n"
	updated, cf := ModifyFile("tests/page/sample.spec.ts", in)
	if cf.IsolatedContextNormalizations != 1 {
		t.Errorf("normalizations = %d, want 1:\n%s", cf.IsolatedContextNormalizations, updated)
	}
	if !strings.Contains(updated, "page.evaluate('x', { a: 1 }, undefined, false)") {
		t.Errorf("not normalized:\n%s", updated)
	}
}

func TestFixmeInsertion(t *testing.T) {
	in := "it('should support webgl @smoke', async ({ page }) => {\n  await page.goto('/');\n});\n"
	updated, cf := ModifyFile("tests/library/capabilities.spec.ts", in)
	if cf.FixmeInsertions != 1 {
		t.Errorf("fixmes = %d, want 1:\n%s", cf.FixmeInsertions, updated)
	}
	if !strings.Contains(updated, "it.fixme(true, ") {
		t.Errorf("marker missing:\n%s", updated)
	}
}

func TestWorkaroundReplace(t *testing.T) {
	in := "  it.fail(browserName === 'chromium', 'Set-Cookie is missing in response after interception');\n  it('x', async () => {});\n"
	updated, cf := ModifyFile("tests/page/page-request-fulfill.spec.ts", in)
	if cf.PatchrightWorkaround != 1 {
		t.Errorf("workaround = %d, want 1", cf.PatchrightWorkaround)
	}
	if strings.Contains(updated, "Set-Cookie is missing") {
		t.Errorf("workaround not applied:\n%s", updated)
	}
}

func TestFixmeTablesComplete(t *testing.T) {
	if len(FixmeTargets) == 0 || len(FixmeTargetFiles) != 5 {
		t.Errorf("fixme tables incomplete: %d files + %d targeted", len(FixmeTargetFiles), len(FixmeTargets))
	}
	if len(Workarounds) == 0 {
		t.Error("workaround table empty")
	}
}
