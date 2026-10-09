package patcher

import (
	"strings"
	"testing"
)

// The TS sources export 34 driver patch functions + 10 client patch functions
// + the rebranding types helper; the Go registry mirrors them 1:1 plus the
// Go-specific patchProtocol / patchRebrandPackages / patchClientTypes units.
var expectedPatches = []string{
	// driver_patches/ (34)
	"patchBrowserContext", "patchBuild", "patchChromium", "patchChromiumSwitches",
	"patchCRBrowser", "patchCRDevTools", "patchCRExecutionContext", "patchCredentials",
	"patchCRNetworkManager", "patchCRCoverage", "patchCRServiceWorker", "patchFrames",
	"patchFrameSelectors", "patchCRPage", "patchPage", "patchUtilityScriptSerializers",
	"patchUtilityScript", "patchPageBinding", "patchClock", "patchCliAlias",
	"patchJavascript", "patchLaunchApp", "patchFrameDispatcher", "patchBrowserContextDispatcher",
	"patchNetwork", "patchNetworkDispatchers", "patchJSHandleDispatcher", "patchPageDispatcher",
	"patchXPathSelectorEngine", "patchRecorder", "patchScreenshotter", "patchSnapshotter",
	"patchSnapshotterInjected", "patchTracing",
	// client_patches/ (10, Go uses patchClient* names)
	"patchClientBrowserContext", "patchClientHelper", "patchClientClock", "patchClientFrame",
	"patchClientJsHandle", "patchClientLocator", "patchClientNetwork", "patchClientPage",
	"patchClientTracing", "patchClientWorker",
	// Go-specific units
	"patchProtocol", "patchRebrandPackages", "patchClientTypes",
}

func TestRegistryCompleteness(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range Registry {
		if seen[p.Name] {
			t.Errorf("duplicate patch %q", p.Name)
		}
		seen[p.Name] = true
	}
	for _, want := range expectedPatches {
		if !seen[want] {
			t.Errorf("missing patch %q", want)
		}
	}
	if len(Registry) != len(expectedPatches) {
		t.Errorf("registry has %d patches, want %d", len(Registry), len(expectedPatches))
	}
}

func TestBodiesLoad(t *testing.T) {
	m, err := Manifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(m) == 0 {
		t.Fatal("empty manifest")
	}
	for _, e := range m {
		body := MustBody(strings.TrimSuffix(e.BodyFile, ".txt"))
		if len(body) == 0 {
			t.Errorf("empty body %s", e.BodyFile)
		}
	}
}

func TestFilterChromiumSwitches(t *testing.T) {
	in := []string{"--enable-automation", "--disable-popup-blocking", "--disable-extensions", "--headless=new"}
	out := FilterChromiumSwitches(in)
	joined := strings.Join(out, " ")
	for _, gone := range []string{"--enable-automation", "--disable-popup-blocking", "--disable-extensions"} {
		if strings.Contains(joined, gone) {
			t.Errorf("switch %q not removed", gone)
		}
	}
	if !strings.Contains(joined, "--disable-blink-features=AutomationControlled") {
		t.Error("stealth switch not added")
	}
	if !strings.Contains(joined, "--headless=new") {
		t.Error("unrelated switch dropped")
	}
	if len(SwitchesToDisable) != 13 {
		t.Errorf("SwitchesToDisable has %d entries, want 13", len(SwitchesToDisable))
	}
}

func TestProtocolMutations(t *testing.T) {
	doc := `
Frame:
  commands:
    evaluateExpression:
      parameters: {}
`
	updated, err := MutateProtocolYAML(doc, ProtocolMutations()["packages/protocol/spec/frame.yml"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated, "isolatedContext") {
		t.Errorf("isolatedContext not added:\n%s", updated)
	}

	doc2 := `
ContextOptions:
  properties: {}
`
	updated2, err := MutateProtocolYAML(doc2, ProtocolMutations()["packages/protocol/spec/mixins.yml"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated2, "focusControl") {
		t.Errorf("focusControl not added:\n%s", updated2)
	}

	doc3 := `
Route:
  commands:
    continue:
      parameters: {}
`
	updated3, err := MutateProtocolYAML(doc3, ProtocolMutations()["packages/protocol/spec/network.yml"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated3, "patchrightInitScript") {
		t.Errorf("patchrightInitScript not added:\n%s", updated3)
	}
}

func TestRebrandTables(t *testing.T) {
	if len(RebrandTables) != 4 {
		t.Errorf("RebrandTables has %d files, want 4", len(RebrandTables))
	}
	sample := "Playwright version: 1.60.0 /     npx playwright install /     npm install @playwright/test"
	got := ReplaceAll(sample, RebrandTables["packages/playwright-core/src/cli/program.ts"])
	if strings.Contains(got, "npx playwright install") || strings.Contains(got, "@playwright/test") {
		t.Errorf("rebrand incomplete: %q", got)
	}
	if !strings.Contains(got, "Patchright version:") || !strings.Contains(got, "npx patchright install") {
		t.Errorf("rebrand wrong: %q", got)
	}
}

func TestChromiumSwitchesPatchAnchors(t *testing.T) {
	// Synthetic chromiumSwitches.ts exercising the add/remove logic.
	// It must contain every switch the patch removes (mirrors upstream).
	lines := []string{"export const chromiumSwitches = () => [", "\t\t'--headless=new',"}
	for _, sw := range SwitchesToDisable {
		lines = append(lines, "\t\t"+sw+",")
	}
	lines = append(lines, "];")
	src := strings.Join(lines, "\n") + "\n"
	fs := &FileSet{files: map[string]string{chromiumSwitchesFile: src}, dirty: map[string]bool{}}
	if err := ApplyNames(fs, "patchChromiumSwitches"); err != nil {
		t.Fatal(err)
	}
	out := fs.files[chromiumSwitchesFile]
	if strings.Contains(out, "--disable-popup-blocking") || strings.Contains(out, "--enable-automation") {
		t.Errorf("switches not removed:\n%s", out)
	}
	if !strings.Contains(out, "--disable-blink-features=AutomationControlled") {
		t.Errorf("stealth switch not added:\n%s", out)
	}
}

func TestLaunchAppPatch(t *testing.T) {
	src := "export function syncLocalStorageWithSettings() {\n\t(window as any)._saveSerializedSettings(JSON.stringify({ ...localStorage }));\n}\n"
	rel := "packages/playwright-core/src/server/launchApp.ts"
	fs := &FileSet{files: map[string]string{rel: src}, dirty: map[string]bool{}}
	if err := ApplyNames(fs, "patchLaunchApp"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fs.files[rel], "typeof (window as any)._saveSerializedSettings === 'function'") {
		t.Errorf("guard not added:\n%s", fs.files[rel])
	}
}

func TestBrowserContextPatch(t *testing.T) {
	src := "class BrowserContext {\n" +
		"\tasync initialize() {\n\t\tthis.doAddInitScript(new InitScript(this, `navigator.serviceWorker.register('x')`));\n\t}\n" +
		"\tasync exposeBinding(binding: any) {\n\t\tawait this.doAddInitScript(binding.initScript);\n\t}\n" +
		"\tasync removeExposedBinding(binding: any) {\n\t\tthis._pageBindings.delete(binding.name);\n\t}\n}\n" +
		"export const defaultNewContextParamValues = {\n\tstrictSelectors: false,\n};\n"
	rel := "packages/playwright-core/src/server/browserContext.ts"
	fs := &FileSet{files: map[string]string{rel: src}, dirty: map[string]bool{}}
	if err := ApplyNames(fs, "patchBrowserContext"); err != nil {
		t.Fatal(err)
	}
	out := fs.files[rel]
	for _, want := range []string{
		"navigator.serviceWorker.register = async () => { };",
		"if (!binding.noGlobal)",
		"disposeFunctionCallbacks",
		"focusControl: false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "navigator.serviceWorker.register('x')") {
		t.Errorf("leaky init script remains:\n%s", out)
	}
}

func TestPlaywrightVersionPin(t *testing.T) {
	if DefaultPlaywrightVersion == "" {
		t.Error("DefaultPlaywrightVersion must not be empty")
	}
	if PlaywrightVersion != DefaultPlaywrightVersion {
		t.Errorf("PlaywrightVersion = %q, want default %q", PlaywrightVersion, DefaultPlaywrightVersion)
	}
}
