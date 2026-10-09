package patcher

import (
	"fmt"
	"strings"
)

// This file ports the medium driver patches: browserContextPatch,
// clockPatch, javascriptPatch, crExecutionContextPatch, crBrowserPatch,
// crServiceWorkerPatch, utilityScriptPatch, XPathSelectorEnginePatch,
// tracingPatch, screenshotterPatch, snapshotterPatch,
// snapshotterInjectedPatch, pageBindingPatch, frameDispatcherPatch.

func init() {
	register(PatchFunc{
		Name:   "patchBrowserContext",
		Target: "packages/playwright-core/src/server/browserContext.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/browserContext.ts"
			text := fs.MustGet(rel)
			// Service-worker registration init script -> no-op stealth script.
			var err error
			text, err = regexpReplace(text,
				"`[^`]*navigator\\.serviceWorker\\.register[^`]*`",
				"`if (navigator.serviceWorker) navigator.serviceWorker.register = async () => { };`")
			if err != nil {
				return fmt.Errorf("serviceWorker init script: %w", err)
			}
			// exposeBinding: gate doAddInitScript behind !binding.noGlobal.
			mustContain(text, "this.doAddInitScript(binding.initScript)", "exposeBinding initScript")
			text = strings.ReplaceAll(text,
				"await this.doAddInitScript(binding.initScript);",
				"if (!binding.noGlobal)\n\t\tawait this.doExposeBinding(binding);")
			// Remove stalling-eval + playwright-binding exposure statements.
			for _, stmt := range []string{
				"this.safeNonStallingEvaluateInAllFrames(binding.initScript.source, 'main')",
				"this.exposePlaywrightBindingIfNeeded()",
			} {
				if strings.Contains(text, stmt) {
					text, err = removeLineContaining(text, stmt)
					if err != nil {
						return err
					}
				}
			}
			// removeExposedBinding: early dispose for noGlobal bindings.
			mustContain(text, "this._pageBindings.delete", "removeExposedBinding")
			text, err = regexpReplace(text,
				`(this\._pageBindings\.delete\([^;]*;[^}]*?\n)`,
				"$1\t\tif (binding.noGlobal) {\n\t\t\tawait binding.disposeFunctionCallbacks();\n\t\t\treturn;\n\t\t}\n")
			if err != nil {
				return fmt.Errorf("removeExposedBinding: %w", err)
			}
			// defaultNewContextParamValues: add focusControl: false.
			if !strings.Contains(text, "focusControl") {
				mustContain(text, "defaultNewContextParamValues", "default context values")
				text, err = regexpReplace(text,
					`((?:export\s+)?const defaultNewContextParamValues[^=]*=\s*\{)`,
					"$1\n\tfocusControl: false,")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClock",
		Target: "packages/playwright-core/src/server/clock.ts",
		Apply: func(fs *FileSet) error {
			return ApplyClock(fs.MustGet("packages/playwright-core/src/server/clock.ts"), fs, "packages/playwright-core/src/server/clock.ts")
		},
	})

	register(PatchFunc{
		Name:   "patchJavascript",
		Target: "packages/playwright-core/src/server/javascript.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/javascript.ts"
			text := fs.MustGet(rel)
			// ExecutionContextDelegate.findFunctions? + ExecutionContext.findFunctions.
			if !strings.Contains(text, "findFunctions(context: ExecutionContext") {
				mustContain(text, "interface ExecutionContextDelegate", "delegate iface")
				var err error
				text, err = regexpReplace(text,
					`(interface ExecutionContextDelegate\s*\{)`,
					"$1\n\tfindFunctions?(context: ExecutionContext, name: string): Promise<JSHandle[]>;")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "async findFunctions(name: string)") {
				var err error
				text, err = appendToClassEnd(text, "class ExecutionContext",
					"\tasync findFunctions(name: string): Promise<JSHandle[]> {"+
						MustBody("patchJavascript_statements_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// JSHandle.evaluateExpression(+Handle): isolatedContext param + body.
			for i, method := range []string{"evaluateExpression", "evaluateExpressionHandle"} {
				bodyID := []string{
					"patchJavascript_setBodyText_01",
					"patchJavascript_setBodyText_02",
				}[i]
				var err error
				text, err = replaceMethodBody(text, method+"(", MustBody(bodyID))
				if err != nil {
					return fmt.Errorf("%s: %w", method, err)
				}
				// Add the optional parameter to the signature line.
				if !strings.Contains(text, "isolatedContext?: boolean") {
					text, err = regexpReplace(text,
						`(async `+method+`\([^)]*)\)`,
						"$1, isolatedContext?: boolean)")
					if err != nil {
						return fmt.Errorf("%s param: %w", method, err)
					}
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCRExecutionContext",
		Target: "packages/playwright-core/src/server/chromium/crExecutionContext.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crExecutionContext.ts"
			text := fs.MustGet(rel)
			if !strings.Contains(text, "async findFunctions(") {
				var err error
				text, err = appendToClassEnd(text, "class CRExecutionContext",
					"\tasync findFunctions(context: js.ExecutionContext, name: string): Promise<js.JSHandle[]> {"+
						MustBody("patchCRExecutionContext_statements_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCRBrowser",
		Target: "packages/playwright-core/src/server/chromium/crBrowser.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crBrowser.ts"
			text := fs.MustGet(rel)
			var err error
			text, err = replaceMethodBody(text, "doRemoveInitScripts",
				MustBody("patchCRBrowser_setBodyText_01"))
			if err != nil {
				return err
			}
			if !strings.Contains(text, "async doExposeBinding(") {
				text, err = appendToClassEnd(text, "class CRBrowserContext",
					"\tasync doExposeBinding(binding: PageBinding) {"+
						MustBody("patchCRBrowser_setBodyText_02")+"\n\t}"+
						"\n\tasync doRemoveExposedBindings() {"+
						MustBody("patchCRBrowser_setBodyText_03")+"\n\t}")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCRServiceWorker",
		Target: "packages/playwright-core/src/server/chromium/crServiceWorker.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crServiceWorker.ts"
			text := fs.MustGet(rel)
			// Remove Runtime.enable from the constructor.
			if strings.Contains(text, "Runtime.enable") {
				lines := strings.Split(text, "\n")
				kept := lines[:0]
				removed := false
				for _, line := range lines {
					if !removed && strings.Contains(line, "session.send") && strings.Contains(line, "Runtime.enable") {
						removed = true
						continue
					}
					kept = append(kept, line)
				}
				if !removed {
					return fmt.Errorf("Runtime.enable ctor statement not found")
				}
				text = strings.Join(kept, "\n")
			}
			// Add globalThis-based execution context bootstrap.
			if !strings.Contains(text, `expression: "globalThis"`) {
				var err error
				text, err = regexpReplace(text,
					`(constructor\(browserContext[^)]*\)\s*\{)`,
					"$1"+MustBody("patchCRServiceWorker_addStatements_01"))
				if err != nil {
					// Fall back to appending inside the constructor via class anchor.
					return fmt.Errorf("serviceWorker ctor: %w", err)
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchUtilityScript",
		Target: "packages/injected/src/utilityScript.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/injected/src/utilityScript.ts"
			text := fs.MustGet(rel)
			if !strings.Contains(text, "_functionCallbacks") {
				var err error
				text, err = regexpReplace(text,
					`((?:private|public|readonly)[^;]*?\bglobal\b[^;]*;)`,
					"$1\n\tprivate _functionCallbacks = new Map<string, Function>();")
				if err != nil {
					return err
				}
			}
			// evaluate(): pass function registry args to parseEvaluationResultValue.
			mustContain(text, "parseEvaluationResultValue", "utility evaluate")
			if !strings.Contains(text, "this._functionCallbacks.set(name, callback)") {
				var err error
				text, err = regexpReplace(text,
					`parseEvaluationResultValue\(([^)]*)\)`,
					"parseEvaluationResultValue($1, new Map(), (name, callback) => this._functionCallbacks.set(name, callback))")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "takeFunctionCallback") {
				var err error
				text, err = appendToClassEnd(text, "class UtilityScript",
					"\ttakeFunctionCallback(name: string): Function | undefined {"+
						MustBody("patchUtilityScript_statements_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchXPathSelectorEngine",
		Target: "packages/injected/src/xpathSelectorEngine.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/injected/src/xpathSelectorEngine.ts"
			text := fs.MustGet(rel)
			if strings.Contains(text, "DOCUMENT_FRAGMENT_NODE") && strings.Contains(text, "Custom ClosedShadowRoot XPath Engine") {
				return nil // already applied
			}
			mustContain(text, "queryAll", "XPathEngine.queryAll")
			body := MustBody("patchXPathSelectorEngine_insertStatements_01")
			var err error
			text, err = regexpReplace(text,
				`(\bqueryAll\(async[^{]*\{|queryAll\([^)]*\)\s*\{)`,
				"$1"+body)
			if err != nil {
				// Fall back: insert after the queryAll property line.
				text2, err2 := insertAfterAnchor(fs.MustGet(rel), "queryAll", body)
				if err2 != nil {
					return fmt.Errorf("xpath queryAll: %v / %v", err, err2)
				}
				text = text2
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchTracing",
		Target: "packages/playwright-core/src/server/trace/recorder/tracing.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/trace/recorder/tracing.ts"
			text := fs.MustGet(rel)
			guard := "if (metadata.type === 'Route' && metadata.method === 'continue' && metadata.params?.isFallback)"
			if strings.Contains(text, guard) {
				return nil
			}
			for _, fn := range []string{
				"createBeforeActionTraceEvent",
				"createInputActionTraceEvent",
				"createActionLogTraceEvent",
				"createAfterActionTraceEvent",
			} {
				var err error
				text, err = regexpReplace(text,
					`(function `+fn+`[^{]*\{)`,
					"$1\n\t\t\t\t// Filter out internal fallback Route.continue calls from Patchright's inject routing\n\t\t\t\tif (metadata.type === 'Route' && metadata.method === 'continue' && metadata.params?.isFallback)\n\t\t\t\t\treturn null;")
				if err != nil {
					return fmt.Errorf("%s: %w", fn, err)
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchScreenshotter",
		Target: "packages/playwright-core/src/server/screenshotter.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/screenshotter.ts"
			text := fs.MustGet(rel)
			if strings.Contains(text, "f.utilityContext()") {
				return nil
			}
			var err error
			text, err = insertBeforeAnchor(text, "safeNonStallingEvaluateInAllFrames",
				"\t\t\tawait Promise.all(this._page.frames().map(async (f: any) => {\n\t\t\t\ttry { await f.utilityContext(); } catch {}\n\t\t\t}));")
			if err != nil {
				return err
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchSnapshotter",
		Target: "packages/playwright-core/src/server/trace/recorder/snapshotter.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/trace/recorder/snapshotter.ts"
			text := fs.MustGet(rel)
			// Remove InitScript import (incl. `import type` form).
			var err error
			text, err = regexpReplace(text, `\s*import(\s+type)?\s*\{[^}]*InitScript[^}]*\}\s*from[^;]*;`, "")
			if err != nil {
				return fmt.Errorf("snapshotter InitScript import: %w", err)
			}
			// _initScript: InitScript|undefined -> boolean|undefined; add source field.
			text, err = regexpReplace(text, `_initScript\??:\s*InitScript[^;]*;`, "_initScript: boolean | undefined;")
			if err != nil {
				text, err = regexpReplace(text, `(_initScript[^;{]*;)`, "_initScript: boolean | undefined;")
				if err != nil {
					return fmt.Errorf("snapshotter _initScript prop: %w", err)
				}
			}
			if !strings.Contains(text, "_initScriptSource") {
				mustContain(text, "_initScript", "snapshotter prop anchor")
				text, err = regexpReplace(text,
					`(_initScript:\s*boolean\s*\|\s*undefined\s*;)`,
					"$1\n\tprivate _initScriptSource: string | undefined;")
				if err != nil {
					return err
				}
			}
			// reset(): switch 'main' world to 'utility'.
			text, err = regexpReplace(text, `'main'`, "'utility'")
			if err != nil {
				return fmt.Errorf("snapshotter reset world: %w", err)
			}
			// _initialize / resetForReuse bodies.
			text, err = replaceMethodBody(text, "_initialize(",
				MustBody("patchSnapshotter_setBodyText_01"))
			if err != nil {
				return fmt.Errorf("snapshotter _initialize: %w", err)
			}
			text, err = replaceMethodBody(text, "resetForReuse(",
				MustBody("patchSnapshotter_setBodyText_02"))
			if err != nil {
				return fmt.Errorf("snapshotter resetForReuse: %w", err)
			}
			// _captureFrameSnapshot: utility-world evaluate.
			mustContain(text, "nonStallingRawEvaluateInExistingMainContext", "snapshotter capture")
			text, err = regexpReplace(text,
				`await\s+frame\.nonStallingRawEvaluateInExistingMainContext\(([^)]*)\)`,
				"await frame.nonStallingEvaluateInExistingContext($1, 'utility')")
			if err != nil {
				return fmt.Errorf("snapshotter capture: %w", err)
			}
			// _onPage: re-inject streamer on navigation.
			mustContain(text, "_onPage(", "snapshotter _onPage")
			text, err = regexpReplace(text,
				`(_onPage\([^)]*\)\s*\{)`,
				"$1\n\t\tthis._eventListeners.push(eventsHelper.addEventListener(page, Page.Events.InternalFrameNavigatedToNewDocument, (frame: Frame) => this._onFrameNavigated(frame)));")
			if err != nil {
				return err
			}
			// _onFrameNavigated: new method.
			if !strings.Contains(text, "async _onFrameNavigated(") {
				text, err = appendToClassEnd(text, "class Snapshotter",
					"\tasync _onFrameNavigated(frame: Frame) {"+MustBody("patchSnapshotter_setBodyText_03")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// _annotateFrameHierarchy: mainContext -> utilityContext.
			mustContain(text, "mainContext", "snapshotter annotate")
			text = strings.Replace(text, "mainContext", "utilityContext", 1)
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchSnapshotterInjected",
		Target: "packages/playwright-core/src/server/trace/recorder/snapshotterInjected.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/trace/recorder/snapshotterInjected.ts"
			text := fs.MustGet(rel)
			for _, stmt := range []string{
				"invalidateCSSGroupingRule",
				"this._interceptNativeMethod",
				"this._interceptNativeGetter",
				"this._interceptNativeAsyncMethod",
			} {
				for strings.Contains(text, stmt+"(") {
					var err error
					text, err = removeLineContaining(text, stmt)
					if err != nil {
						break
					}
				}
			}
			for _, member := range []string{
				"_staleStyleSheets", "_readingStyleSheet",
				"_interceptNativeMethod", "_interceptNativeAsyncMethod",
				"_interceptNativeGetter", "_invalidateStyleSheet",
			} {
				_ = member
			}
			var err error
			text, err = replaceMethodBody(text, "_updateStyleElementStyleSheetTextIfNeeded",
				MustBody("patchSnapshotterInjected_setBodyText_01"))
			if err != nil {
				return err
			}
			text, err = replaceMethodBody(text, "_updateLinkStyleSheetTextIfNeeded",
				MustBody("patchSnapshotterInjected_setBodyText_02"))
			if err != nil {
				return err
			}
			text, err = replaceMethodBody(text, "_getSheetText",
				MustBody("patchSnapshotterInjected_setBodyText_03"))
			if err != nil {
				return err
			}
			mustContain(text, "this._modifiedStyleSheets", "captureSnapshot sheets")
			text = strings.ReplaceAll(text, "this._modifiedStyleSheets", "document.styleSheets")
			if strings.Contains(text, "this._staleStyleSheets.clear();") {
				var err error
				text, err = removeLineContaining(text, "this._staleStyleSheets.clear();")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchPageBinding",
		Target: "packages/playwright-core/src/server/pageBinding.ts",
		Apply: func(fs *FileSet) error {
			// Whole-file replacement with the patchright pageBinding source.
			const rel = "packages/playwright-core/src/server/pageBinding.ts"
			fs.Create(rel, MustBody("patchPageBinding_createSourceFile_01"))
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchFrameDispatcher",
		Target: "packages/playwright-core/src/server/dispatchers/frameDispatcher.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/dispatchers/frameDispatcher.ts"
			text := fs.MustGet(rel)
			var err error
			text, err = replaceMethodBody(text, "evaluateExpression(",
				MustBody("patchFrameDispatcher_setBodyText_01"))
			if err != nil {
				return err
			}
			// evaluateExpressionHandle is the second method with a similar prefix;
			// anchor on the exact declaration via generic-aware lookup.
			mIdx := findMethodDecl(text, "evaluateExpressionHandle")
			if mIdx < 0 {
				return fmt.Errorf("evaluateExpressionHandle not found")
			}
			open := strings.Index(text[mIdx:], "{")
			if open < 0 {
				return fmt.Errorf("evaluateExpressionHandle body not found")
			}
			open += mIdx
			end, err2 := braceBlockEnd(text, open)
			if err2 != nil {
				return err2
			}
			text = text[:open+1] + MustBody("patchFrameDispatcher_setBodyText_02") + text[end-1:]
			text, err = replaceMethodBody(text, "evalOnSelectorAll(",
				MustBody("patchFrameDispatcher_setBodyText_03"))
			if err != nil {
				return err
			}
			fs.Set(rel, text)
			return nil
		},
	})
}
