// Package impact answers "did upstream break our patches again": symbol table
// of everything we touch, diff two playwright versions against it, spit out
// the JSON report + markdown + affected diff. same stuff the CI eats.
package impact

import (
	"sort"
	"strings"

	"github.com/notlousybook/patchright-go/patcher"
)

// Kind mirrors PatchedSymbolKind.
type Kind string

const (
	KindClass            Kind = "class"
	KindMethod           Kind = "method"
	KindProperty         Kind = "property"
	KindFunction         Kind = "function"
	KindParameter        Kind = "parameter"
	KindProtocolParam    Kind = "protocol_param"
	KindProtocolProperty Kind = "protocol_property"
)

// Record mirrors PatchedSymbolRecord.
type Record struct {
	Symbol                  string `json:"symbol"`
	Kind                    Kind   `json:"kind"`
	PlaywrightFile          string `json:"playwrightFile"`
	PatchFile               string `json:"patchFile"`
	PatchFileLineStart      *int   `json:"patchFileLineStart"`
	PatchFileLineEnd        *int   `json:"patchFileLineEnd"`
	PlaywrightFileLineStart *int   `json:"playwrightFileLineStart"`
	PlaywrightFileLineEnd   *int   `json:"playwrightFileLineEnd"`
}

// RelevantPathPrefixes mirrors RELEVANT_PATH_PREFIXES.
var RelevantPathPrefixes = []string{
	"packages/playwright-core/src/server/",
	"packages/playwright-core/src/utils/isomorphic/",
	"packages/playwright-core/src/injected/src/",
	"packages/injected/src/",
	"packages/playwright-core/src/server/dispatchers/",
	"packages/playwright-core/src/recorder/src/",
	"packages/recorder/src/",
	"packages/protocol/src/",
}

// IsRelevantPath mirrors isRelevantPath.
func IsRelevantPath(filePath string) bool {
	for _, prefix := range RelevantPathPrefixes {
		if strings.HasPrefix(filePath, prefix) {
			return true
		}
	}
	return false
}

// symbolSpec is one static symbol entry: the upstream symbol a patch touches.
type symbolSpec struct {
	symbol         string
	kind           Kind
	playwrightFile string
	patchName      string // Go PatchFunc name (PatchFile equivalent)
}

// SymbolSpecs is the static extraction of what extract_patched_symbols.ts
// derives dynamically from the TS patch sources via getXOrThrow("symbol")
// calls. Each entry names the upstream symbol a Go patch transforms; the
// patch file is identified by Go patch name.
var SymbolSpecs = []symbolSpec{
	// patchBrowserContext.
	{"doAddInitScript", KindMethod, "packages/playwright-core/src/server/browserContext.ts", "patchBrowserContext"},
	{"exposeBinding", KindMethod, "packages/playwright-core/src/server/browserContext.ts", "patchBrowserContext"},
	{"removeExposedBinding", KindMethod, "packages/playwright-core/src/server/browserContext.ts", "patchBrowserContext"},
	{"defaultNewContextParamValues", KindFunction, "packages/playwright-core/src/server/browserContext.ts", "patchBrowserContext"},
	// patchChromium.
	{"_innerDefaultArgs", KindMethod, "packages/playwright-core/src/server/chromium/chromium.ts", "patchChromium"},
	// patchChromiumSwitches: whole-array transform.
	{"chromiumSwitches", KindFunction, "packages/playwright-core/src/server/chromium/chromiumSwitches.ts", "patchChromiumSwitches"},
	// patchCRBrowser.
	{"doRemoveInitScripts", KindMethod, "packages/playwright-core/src/server/chromium/crBrowser.ts", "patchCRBrowser"},
	{"doExposeBinding", KindMethod, "packages/playwright-core/src/server/chromium/crBrowser.ts", "patchCRBrowser"},
	{"doRemoveExposedBindings", KindMethod, "packages/playwright-core/src/server/chromium/crBrowser.ts", "patchCRBrowser"},
	// patchCRDevTools.
	{"install", KindMethod, "packages/playwright-core/src/server/chromium/crDevTools.ts", "patchCRDevTools"},
	// patchCRExecutionContext.
	{"findFunctions", KindMethod, "packages/playwright-core/src/server/chromium/crExecutionContext.ts", "patchCRExecutionContext"},
	// patchCRNetworkManager.
	{"CRNetworkManager", KindClass, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_onRequest", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_onRequestPaused", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_onRequestServedFromCache", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_onLoadingFinished", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"setRequestInterception", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"fulfill", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"continue", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_networkRequestIntercepted", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_fixCSP", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	{"_injectIntoHead", KindMethod, "packages/playwright-core/src/server/chromium/crNetworkManager.ts", "patchCRNetworkManager"},
	// patchCRCoverage.
	{"JSCoverage", KindClass, "packages/playwright-core/src/server/chromium/crCoverage.ts", "patchCRCoverage"},
	{"CSSCoverage", KindClass, "packages/playwright-core/src/server/chromium/crCoverage.ts", "patchCRCoverage"},
	// patchCRServiceWorker.
	{"CRServiceWorker", KindClass, "packages/playwright-core/src/server/chromium/crServiceWorker.ts", "patchCRServiceWorker"},
	// patchFrames.
	{"Frame", KindClass, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"evalOnSelector", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"evalOnSelectorAll", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"dispatchEvent", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"querySelectorAll", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"querySelector", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"evaluateExpression", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"evaluateExpressionHandle", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"context", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"existingContext", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"setContent", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	{"waitForSelector", KindMethod, "packages/playwright-core/src/server/frames.ts", "patchFrames"},
	// patchFrameSelectors.
	{"FrameSelectors", KindClass, "packages/playwright-core/src/server/frameSelectors.ts", "patchFrameSelectors"},
	{"queryArrayInMainWorld", KindMethod, "packages/playwright-core/src/server/frameSelectors.ts", "patchFrameSelectors"},
	{"resolveInjectedForSelector", KindMethod, "packages/playwright-core/src/server/frameSelectors.ts", "patchFrameSelectors"},
	// patchCRPage.
	{"CRPage", KindClass, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"FrameSession", KindClass, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_initialize", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_onFrameNavigated", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_onExecutionContextCreated", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_onBindingCalled", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_evaluateOnNewDocument", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_removeEvaluatesOnNewDocument", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_onLifecycleEvent", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	{"_onDialog", KindMethod, "packages/playwright-core/src/server/chromium/crPage.ts", "patchCRPage"},
	// patchPage.
	{"Page", KindClass, "packages/playwright-core/src/server/page.ts", "patchPage"},
	{"PageBinding", KindClass, "packages/playwright-core/src/server/page.ts", "patchPage"},
	{"exposeBinding", KindMethod, "packages/playwright-core/src/server/page.ts", "patchPage"},
	{"removeExposedBinding", KindMethod, "packages/playwright-core/src/server/page.ts", "patchPage"},
	{"allInitScripts", KindMethod, "packages/playwright-core/src/server/page.ts", "patchPage"},
	{"Worker", KindClass, "packages/playwright-core/src/server/page.ts", "patchPage"},
	// patchUtilityScript.
	{"UtilityScript", KindClass, "packages/injected/src/utilityScript.ts", "patchUtilityScript"},
	// patchUtilityScriptSerializers.
	{"parseEvaluationResultValue", KindFunction, "packages/isomorphic/utilityScriptSerializers.ts", "patchUtilityScriptSerializers"},
	// patchClock.
	{"_installIfNeeded", KindMethod, "packages/playwright-core/src/server/clock.ts", "patchClock"},
	{"_evaluateInFrames", KindMethod, "packages/playwright-core/src/server/clock.ts", "patchClock"},
	// patchJavascript.
	{"evaluateExpression", KindMethod, "packages/playwright-core/src/server/javascript.ts", "patchJavascript"},
	{"evaluateExpressionHandle", KindMethod, "packages/playwright-core/src/server/javascript.ts", "patchJavascript"},
	// patchPageBinding.
	{"createPageBindingScript", KindFunction, "packages/playwright-core/src/server/pageBinding.ts", "patchPageBinding"},
	// patchXPathSelectorEngine.
	{"XPathEngine", KindClass, "packages/injected/src/xpathSelectorEngine.ts", "patchXPathSelectorEngine"},
	// patchRecorder.
	{"Recorder", KindClass, "packages/recorder/src/recorder.tsx", "patchRecorder"},
	// patchScreenshotter is positional (no named symbol); record the class.
	{"Screenshotter", KindClass, "packages/playwright-core/src/server/screenshotter.ts", "patchScreenshotter"},
	// patchSnapshotter.
	{"Snapshotter", KindClass, "packages/playwright-core/src/server/trace/recorder/snapshotter.ts", "patchSnapshotter"},
	{"_initialize", KindMethod, "packages/playwright-core/src/server/trace/recorder/snapshotter.ts", "patchSnapshotter"},
	{"resetForReuse", KindMethod, "packages/playwright-core/src/server/trace/recorder/snapshotter.ts", "patchSnapshotter"},
	// patchSnapshotterInjected.
	{"_updateStyleElementStyleSheetTextIfNeeded", KindMethod, "packages/playwright-core/src/server/trace/recorder/snapshotterInjected.ts", "patchSnapshotterInjected"},
	{"_updateLinkStyleSheetTextIfNeeded", KindMethod, "packages/playwright-core/src/server/trace/recorder/snapshotterInjected.ts", "patchSnapshotterInjected"},
	{"_getSheetText", KindMethod, "packages/playwright-core/src/server/trace/recorder/snapshotterInjected.ts", "patchSnapshotterInjected"},
	// patchTracing is positional (four guard functions share one shape).
	{"createBeforeActionTraceEvent", KindFunction, "packages/playwright-core/src/server/trace/recorder/tracing.ts", "patchTracing"},
	// Dispatchers.
	{"FrameDispatcher", KindClass, "packages/playwright-core/src/server/dispatchers/frameDispatcher.ts", "patchFrameDispatcher"},
	{"BrowserContextDispatcher", KindClass, "packages/playwright-core/src/server/dispatchers/browserContextDispatcher.ts", "patchBrowserContextDispatcher"},
	{"continue", KindMethod, "packages/playwright-core/src/server/dispatchers/networkDispatchers.ts", "patchNetworkDispatchers"},
	{"evaluateExpression", KindMethod, "packages/playwright-core/src/server/dispatchers/jsHandleDispatcher.ts", "patchJSHandleDispatcher"},
	{"evaluateExpression", KindMethod, "packages/playwright-core/src/server/dispatchers/pageDispatcher.ts", "patchPageDispatcher"},
	// patchNetwork.
	{"setRawRequestHeaders", KindMethod, "packages/playwright-core/src/server/network.ts", "patchNetwork"},
	// Line-level patches without a named TS symbol target (whole-file scope):
	// cliAlias (CLI strings), credentials (WebAuthn timing), launchApp
	// (storage guard), build (bundle marker). Recorded against their files.
	{"program.ts", KindFunction, "packages/playwright-core/src/cli/program.ts", "patchCliAlias"},
	{"credentials.ts", KindFunction, "packages/playwright-core/src/server/credentials.ts", "patchCredentials"},
	{"syncLocalStorageWithSettings", KindFunction, "packages/playwright-core/src/server/launchApp.ts", "patchLaunchApp"},
	{"build.js", KindFunction, "utils/build/build.js", "patchBuild"},
	// Client patches.
	{"installInjectRoute", KindMethod, "packages/playwright-core/src/client/browserContext.ts", "patchClientBrowserContext"},
	{"installInjectRoute", KindMethod, "packages/playwright-core/src/client/page.ts", "patchClientPage"},
	{"initScriptSourceWithExposedFunctions", KindFunction, "packages/playwright-core/src/client/clientHelper.ts", "patchClientHelper"},
	{"install", KindMethod, "packages/playwright-core/src/client/clock.ts", "patchClientClock"},
	{"evaluate", KindMethod, "packages/playwright-core/src/client/frame.ts", "patchClientFrame"},
	{"evaluate", KindMethod, "packages/playwright-core/src/client/jsHandle.ts", "patchClientJsHandle"},
	{"evaluate", KindMethod, "packages/playwright-core/src/client/locator.ts", "patchClientLocator"},
	{"allHeaders", KindMethod, "packages/playwright-core/src/client/network.ts", "patchClientNetwork"},
	{"evaluate", KindMethod, "packages/playwright-core/src/client/page.ts", "patchClientPage"},
	{"start", KindMethod, "packages/playwright-core/src/client/tracing.ts", "patchClientTracing"},
	{"evaluate", KindMethod, "packages/playwright-core/src/client/worker.ts", "patchClientWorker"},
}

// ProtocolSymbols mirrors PROTOCOL_SYMBOLS.
var ProtocolSymbols = []Record{
	{Symbol: "Frame.evaluateExpression.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "Frame.evaluateExpressionHandle.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "JSHandle.evaluateExpression.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "JSHandle.evaluateExpressionHandle.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "Worker.evaluateExpression.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "Worker.evaluateExpressionHandle.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "Frame.evalOnSelectorAll.parameters.isolatedContext", Kind: KindProtocolParam, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
	{Symbol: "ContextOptions.properties.focusControl", Kind: KindProtocolProperty, PlaywrightFile: "packages/protocol/src/protocol.yml", PatchFile: "patchright_driver_patch.ts"},
}

// ExtractSymbols mirrors extractPatchedSymbols (without the GitHub fetch half):
// it builds the deduped, sorted symbol table from SymbolSpecs + ProtocolSymbols.
func ExtractSymbols() []Record {
	seen := map[string]bool{}
	var out []Record
	for _, s := range SymbolSpecs {
		key := s.symbol + "::" + string(s.kind) + "::" + s.playwrightFile + "::" + s.patchName
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Record{Symbol: s.symbol, Kind: s.kind, PlaywrightFile: s.playwrightFile, PatchFile: s.patchName})
	}
	for _, r := range ProtocolSymbols {
		key := r.Symbol + "::" + string(r.Kind) + "::" + r.PlaywrightFile + "::" + r.PatchFile
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].PlaywrightFile != out[j].PlaywrightFile {
			return out[i].PlaywrightFile < out[j].PlaywrightFile
		}
		if out[i].PatchFile != out[j].PatchFile {
			return out[i].PatchFile < out[j].PatchFile
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}

// SymbolsCoverPatches asserts every registered patch has at least one symbol
// entry (used by tests to keep the table in sync with the registry).
func SymbolsCoverPatches() []string {
	covered := map[string]bool{}
	for _, s := range SymbolSpecs {
		covered[s.patchName] = true
	}
	// Protocol mutations belong to patchProtocol; rebrand/tracing-positional
	// patches are covered by file-level entries.
	covered["patchProtocol"] = true
	covered["patchRebrandPackages"] = true
	covered["patchClientTypes"] = true
	var missing []string
	for _, p := range patcher.Registry {
		if !covered[p.Name] {
			missing = append(missing, p.Name)
		}
	}
	return missing
}
