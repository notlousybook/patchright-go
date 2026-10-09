package patcher

import (
	"fmt"
	"strings"
)

// This file ports patchFrames (server/frames.ts), patchFrameSelectors
// (server/frameSelectors.ts), patchPage (server/page.ts) and
// patchUtilityScriptSerializers (utils/isomorphic/utilityScriptSerializers.ts).

func init() {
	register(PatchFunc{
		Name:   "patchFrames",
		Target: "packages/playwright-core/src/server/frames.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/frames.ts"
			text := fs.MustGet(rel)
			var err error

			// Imports: TargetClosedError, CRExecutionContext, FrameExecutionContext, CRSession, crypto.
			// Upstream uses single quotes; tolerate both.
			if !strings.Contains(text, "TargetClosedError") {
				text, err = regexpReplace(text, `(\} from ['"]\./errors['"];)`, ", TargetClosedError$1")
				if err != nil {
					text, err = regexpReplace(text, `(from ['"]\./errors['"][^;]*;)`, "$1\nimport { TargetClosedError } from './errors';")
					if err != nil {
						return err
					}
				}
			}
			for _, imp := range []string{
				"import { CRExecutionContext } from \"./chromium/crExecutionContext\";",
				"import { FrameExecutionContext } from \"./dom\";",
				"import crypto from \"crypto\";",
			} {
				if !strings.Contains(text, strings.Split(imp, " from ")[1]) {
					text = imp + "\n" + text
				}
			}
			if !strings.Contains(text, "CRSession") {
				text = "import type { CRSession } from \"./chromium/crConnection\";\n" + text
			}
			// FrameManager.frameCommittedNewDocumentNavigation: clear lazy worlds.
			if !strings.Contains(text, "frame._iframeWorld = undefined;") {
				mustContain(text, "frame._onClearLifecycle();", "frameCommittedNewDocumentNavigation")
				text = strings.Replace(text, "frame._onClearLifecycle();",
					"frame._iframeWorld = undefined;\n\t\tframe._mainWorld = undefined;\n\t\tframe._isolatedWorld = undefined;\n"+
						"\t\tframe._iframeWorldContextPromise = undefined;\n\t\tframe._mainWorldContextPromise = undefined;\n"+
						"\t\tframe._isolatedWorldContextPromise = undefined;\n\t\tframe._onClearLifecycle();", 1)
			}
			// Frame properties.
			for _, prop := range []string{
				"_isolatedWorld: dom.FrameExecutionContext",
				"_mainWorld: dom.FrameExecutionContext",
				"_iframeWorld: dom.FrameExecutionContext",
				"_isolatedWorldContextPromise: Promise<number | undefined>",
				"_mainWorldContextPromise: Promise<number | undefined>",
				"_iframeWorldContextPromise: Promise<number | undefined>",
			} {
				name := strings.Split(prop, ":")[0]
				if !strings.Contains(text, name) {
					text, err = regexpReplace(text, `(class Frame[^{]*\{)`, "$1\n\tprivate "+prop+";")
					if err != nil {
						return err
					}
				}
			}
			// Method bodies, in TS application order.
			bodies := [][2]string{
				{"evalOnSelector(", "patchFrames_setBodyText_01"},
				{"evalOnSelectorAll(", "patchFrames_setBodyText_02"},
				{"dispatchEvent(", "patchFrames_setBodyText_03"},
				{"querySelectorAll(", "patchFrames_setBodyText_04"},
				{"querySelector(", "patchFrames_setBodyText_05"},
				{"evaluateExpression(", "patchFrames_setBodyText_16"},
				{"evaluateExpressionHandle(", "patchFrames_setBodyText_17"},
				{"nonStallingEvaluateInExistingContext(", "patchFrames_setBodyText_18"},
				{"queryCount(", "patchFrames_setBodyText_19"},
			}
			for _, b := range bodies {
				text, err = replaceMethodBody(text, b[0], MustBody(b[1]))
				if err != nil {
					return fmt.Errorf("frames %s: %w", b[0], err)
				}
			}
			// _getFrameMainFrameContextId: new method.
			if !strings.Contains(text, "async _getFrameMainFrameContextId(") {
				text, err = appendToClassEnd(text, "class Frame",
					"\tasync _getFrameMainFrameContextId(client: CRSession): Promise<number> {"+
						MustBody("patchFrames_setBodyText_06")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// context -> _context rename + lazy registration body + wrapper.
			// Anchor on the Frame.context declaration (not `async` in v1.60).
			if !strings.Contains(text, "_context(world: types.World)") {
				declIdx := findMethodDecl(text, "context")
				if declIdx < 0 {
					return fmt.Errorf("frames context method not found")
				}
				// Verify it's the Frame.context declaration by checking the
				// signature shape that follows.
				sigEnd, err := parenEnd(text, strings.Index(text[declIdx:], "(")+declIdx)
				if err != nil {
					return fmt.Errorf("frames context sig: %w", err)
				}
				if !strings.Contains(text[declIdx:sigEnd], "types.World") {
					return fmt.Errorf("frames context method not found (shape mismatch)")
				}
				text = text[:declIdx] + "_context" + text[declIdx+len("context"):]
				// Replace the renamed body.
				text, err = replaceMethodBody(text, "_context(",
					MustBody("patchFrames_setBodyText_07"))
				if err != nil {
					return fmt.Errorf("frames _context: %w", err)
				}
				// Add the delegating wrapper after _context.
				cIdx := strings.Index(text, "class Frame")
				mIdx := strings.Index(text[cIdx:], "async _context(")
				mOpen := strings.Index(text[cIdx+mIdx:], "{") + cIdx + mIdx
				mEnd, err := braceBlockEnd(text, mOpen)
				if err != nil {
					return err
				}
				text = text[:mEnd] + "\n\tcontext(world: types.World): Promise<dom.FrameExecutionContext> {\n\t\treturn this._context(world);\n\t}\n" + text[mEnd:]
			}
			// v1.60 renamed existingContext -> _existingMainContext.
			existingAnchor := "existingContext("
			if findMethodDecl(text, "_existingMainContext") >= 0 && findMethodDecl(text, "existingContext") < 0 {
				existingAnchor = "_existingMainContext("
			}
			text, err = replaceMethodBody(text, existingAnchor,
				MustBody("patchFrames_setBodyText_08"))
			if err != nil {
				return fmt.Errorf("frames existingContext: %w", err)
			}
			// HighlightController._resolve: lazy utility context.
			const hlRel = "packages/playwright-core/src/server/highlightController.ts"
			if fs.Has(hlRel) {
				hl := fs.MustGet(hlRel)
				if strings.Contains(hl, "frame.existingContext('utility')") && !strings.Contains(hl, "highlights.length ? await frame.context('utility')") {
					hl = strings.ReplaceAll(hl, "frame.existingContext('utility')",
						"highlights.length ? await frame.context('utility') : frame.existingContext('utility')")
					fs.Set(hlRel, hl)
				}
			}
			for _, b := range [][2]string{
				{"setContent(", "patchFrames_setBodyText_09"},
				{"waitForSelector(", "patchFrames_setBodyText_13"},
				{"isVisibleInternal(", "patchFrames_setBodyText_14"},
				{"_onDetached(", "patchFrames_setBodyText_15"},
			} {
				text, err = replaceMethodBody(text, b[0], MustBody(b[1]))
				if err != nil {
					return fmt.Errorf("frames %s: %w", b[0], err)
				}
			}
			// _retryWithProgressIfNotConnected: signature-dependent bodies.
			if strings.Contains(text, "_retryWithProgressIfNotConnected") && !strings.Contains(text, "_retryWithoutProgress(progress") {
				if strings.Contains(text, "returnAction") {
					// Already has the param; replace body with the matching variant.
					if strings.Contains(text, "performActionPreChecks: boolean") || !strings.Contains(text, "strict, performActionPreChecks") {
						text, err = replaceMethodBody(text, "_retryWithProgressIfNotConnected(",
							MustBody("patchFrames_setBodyText_10"))
					} else {
						text, err = replaceMethodBody(text, "_retryWithProgressIfNotConnected(",
							MustBody("patchFrames_setBodyText_11"))
					}
					if err != nil {
						return fmt.Errorf("frames retry: %w", err)
					}
				} else {
					// Add returnAction param, then apply the options-variant body.
					text, err = regexpReplace(text,
						`(async _retryWithProgressIfNotConnected\([^)]*)(\))`,
						"$1, returnAction: 'returnOnNotResolved' | 'returnAll' | undefined$2")
					if err != nil {
						text, err = regexpReplace(text,
							`(_retryWithProgressIfNotConnected\([^)]*)(\))`,
							"$1, returnAction: 'returnOnNotResolved' | 'returnAll' | undefined$2")
						if err != nil {
							return fmt.Errorf("frames retry param: %w", err)
						}
					}
					text, err = replaceMethodBody(text, "_retryWithProgressIfNotConnected(",
						MustBody("patchFrames_setBodyText_10"))
					if err != nil {
						return fmt.Errorf("frames retry body: %w", err)
					}
				}
			}
			// _retryWithoutProgress + _customFindElementsByParsed: new methods.
			if !strings.Contains(text, "async _retryWithoutProgress(") {
				text, err = appendToClassEnd(text, "class Frame",
					"\tasync _retryWithoutProgress(progress: Progress, selector: string, options: any, action: any, returnAction: any, continuePolling: symbol): Promise<any> {"+
						MustBody("patchFrames_setBodyText_12")+"\n\t}")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "async _customFindElementsByParsed(") {
				text, err = appendToClassEnd(text, "class Frame",
					"\tasync _customFindElementsByParsed(resolved: any, client: CRSession, context: dom.FrameExecutionContext, documentScope: dom.ElementHandle, progress: Progress, parsed: ParsedSelector): Promise<any[]> {"+
						MustBody("patchFrames_setBodyText_21")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// waitForFunctionExpression: _detachedScope race.
			if strings.Contains(text, "progress.race(handle.evaluateHandle(h => h.result))") &&
				!strings.Contains(text, "progress.race(this._detachedScope.race(handle.evaluateHandle(h => h.result)))") {
				text = strings.ReplaceAll(text,
					"progress.race(handle.evaluateHandle(h => h.result))",
					"progress.race(this._detachedScope.race(handle.evaluateHandle(h => h.result)))")
			}
			// _waitForFunctionOnSelector body.
			text, err = replaceMethodBody(text, "_waitForFunctionOnSelector(",
				MustBody("patchFrames_setBodyText_20"))
			if err != nil {
				return fmt.Errorf("frames _waitForFunctionOnSelector: %w", err)
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchFrameSelectors",
		Target: "packages/playwright-core/src/server/frameSelectors.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/frameSelectors.ts"
			text := fs.MustGet(rel)
			var err error

			for _, imp := range []string{
				"import { ElementHandle } from \"./dom\";",
				"import { Progress, nullProgress } from \"./progress\";",
			} {
				if !strings.Contains(text, strings.Split(imp, " from ")[1]) {
					text = imp + "\n" + text
				}
			}
			if !strings.Contains(text, "CRSession") {
				text = "import type { CRSession } from \"./chromium/crConnection\";\n" + text
			}
			if !strings.Contains(text, "Protocol") {
				text = "import type { Protocol } from \"./chromium/protocol\";\n" + text
			}
			// _hasClosedShadowRoots: new method.
			if !strings.Contains(text, "async _hasClosedShadowRoots(") {
				text, err = appendToClassEnd(text, "class FrameSelectors",
					"\tasync _hasClosedShadowRoots(): Promise<boolean> {"+
						MustBody("patchFrameSelectors_setBodyText_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// _callOnSelectorInternal: elements override (older upstream; v1.60
			// removed this method, keeping only resolveInjectedForSelector).
			if strings.Contains(text, "_callOnSelectorInternal(") && !strings.Contains(text, "params.elements ||") {
				mustContain(text, "injected.querySelectorAll", "_callOnSelectorInternal query")
				text = strings.Replace(text, "injected.querySelectorAll",
					"params.elements || injected.querySelectorAll", 1)
				if strings.Contains(text, "returnByValue") {
					text, err = regexpReplace(text,
						`(returnByValue[^,}]*)([,\}])`,
						"$1, elements$2")
					if err != nil {
						return err
					}
				}
				text, err = replaceMethodBody(text, "_callOnSelectorInternal(",
					MustBody("patchFrameSelectors_setBodyText_02"))
				if err != nil {
					return fmt.Errorf("_callOnSelectorInternal: %w", err)
				}
			}
			// queryArrayInMainWorld: isolatedContext param + mainWorld mapping.
			if !strings.Contains(text, "isolatedContext") {
				qIdx := findMethodDecl(text, "queryArrayInMainWorld")
				if qIdx < 0 {
					return fmt.Errorf("queryArrayInMainWorld not found")
				}
				qOpen := strings.Index(text[qIdx:], "(") + qIdx
				updated, err := appendParam(text, qOpen, "isolatedContext?: boolean")
				if err != nil {
					return err
				}
				text = updated
			}
			text = strings.ReplaceAll(text, "mainWorld: true", "mainWorld: !isolatedContext")
			// resolveFrameForSelector: let element + CDP fallback + isConnected.
			text = strings.ReplaceAll(text, "const element = handle?.asElement()", "let element = handle?.asElement()")
			if strings.Contains(text, "if (!element)") && !strings.Contains(text, "_customFindFramesByParsed(await context.injectedScript()") {
				text = strings.Replace(text, "if (!element) {",
					strings.TrimSpace(MustBody("patchFrameSelectors_replaceWithText_01")), 1)
			}
			if strings.Contains(text, "const maybeFrame = await frame._page.delegate.getContentFrame(element)") && !strings.Contains(text, "const isConnected = await element.evaluateInUtility") {
				text = strings.Replace(text, "const maybeFrame = await frame._page.delegate.getContentFrame(element)",
					strings.TrimSpace(MustBody("patchFrameSelectors_insertStatements_01"))+"\n\t\t\tconst maybeFrame = await frame._page.delegate.getContentFrame(element)", 1)
			}
			// noStall context init for both selector-resolution methods.
			if !strings.Contains(text, "noStall ? await frame.raceAgainstEvaluationStallingEvents") {
				text = strings.ReplaceAll(text,
					"await frame.context(info.world)",
					"noStall ? await frame.raceAgainstEvaluationStallingEvents(() => frame.context(info.world)).catch(e => {\n\t\t\tif (e instanceof EvaluationStalledError)\n\t\t\t\treturn null;\n\t\t\tthrow e;\n\t\t}) : await frame.context(info.world)")
				text = strings.ReplaceAll(text,
					"await frame.context(world)",
					"noStall ? await frame.raceAgainstEvaluationStallingEvents(() => frame.context(world)).catch(e => {\n\t\t\tif (e instanceof EvaluationStalledError)\n\t\t\t\treturn null;\n\t\t\tthrow e;\n\t\t}) : await frame.context(world)")
			}
			// resolveInjectedForSelector + _customFindFramesByParsed + _findElementPositionInDomTree.
			if !strings.Contains(text, "async resolveInjectedForSelector(") {
				text, err = appendToClassEnd(text, "class FrameSelectors",
					"\tasync resolveInjectedForSelector(selector: string, options: any, scope?: ElementHandle): Promise<any> {"+
						MustBody("patchFrameSelectors_statements_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "async _customFindFramesByParsed(") {
				text, err = appendToClassEnd(text, "class FrameSelectors",
					"\tasync _customFindFramesByParsed(resolved: any, client: CRSession, context: any, documentScope: ElementHandle, progress: Progress | undefined, parsed: ParsedSelector): Promise<any[]> {"+
						MustBody("patchFrameSelectors_setBodyText_03")+"\n\t}")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "async _findElementPositionInDomTree(") {
				text, err = appendToClassEnd(text, "class FrameSelectors",
					"\tasync _findElementPositionInDomTree(element: { backendNodeId: number }, queryingElement: Protocol.DOM.Node, context: any, currentIndex: string): Promise<string | null> {"+
						MustBody("patchFrameSelectors_setBodyText_04")+"\n\t}")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchPage",
		Target: "packages/playwright-core/src/server/page.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/page.ts"
			text := fs.MustGet(rel)
			var err error

			if !strings.Contains(text, "splitSelectorByFrame") {
				mustContain(text, "@isomorphic/selectorParser", "selectorParser import")
				text = strings.Replace(text, "@isomorphic/selectorParser",
					"@isomorphic/selectorParser\"; // patchright\nimport { splitSelectorByFrame } from \"@isomorphic/selectorParser", 1)
			}
			if !strings.Contains(text, "domValue") {
				text = "import * as domValue from \"./dom\";\n" + text
			}
			if !strings.Contains(text, "createPageBindingScript") {
				text = "import { createPageBindingScript, deliverBindingResult } from \"./pageBinding\";\n" + text
			}
			// Page.exposeBinding body.
			text, err = replaceMethodBody(text, "exposeBinding(",
				MustBody("patchPage_setBodyText_01"))
			if err != nil {
				return fmt.Errorf("page exposeBinding: %w", err)
			}
			// Page.removeExposedBinding: noGlobal early dispose.
			if !strings.Contains(text, "if (binding.noGlobal) {\n\t\t\tawait binding.disposeFunctionCallbacks();") {
				mustContain(text, "this._pageBindings.delete", "removeExposedBinding")
				text = strings.Replace(text, "this._pageBindings.delete",
					"this._pageBindings.delete // patchright anchor", 1)
				// Insert after the delete statement line.
				text, err = insertAfterAnchor(text, "this._pageBindings.delete",
					strings.TrimSpace(MustBody("patchPage_insertStatements_01")))
				if err != nil {
					return err
				}
				text = strings.ReplaceAll(text, " // patchright anchor", "")
			}
			// allInitScripts -> allBindings. Remove the whole method block.
			if strings.Contains(text, "allInitScripts(") {
				updated, err := replaceBlock(text, "allInitScripts(", "", "")
				if err != nil {
					// Fall back: rename so the stale method can't shadow allBindings.
					text = strings.ReplaceAll(text, "allInitScripts(", "allInitScripts_removed(")
				} else {
					// replaceBlock keeps the braces; drop the emptied method head..brace span instead.
					_ = updated
					idx := strings.Index(text, "allInitScripts(")
					lineStart := strings.LastIndex(text[:idx], "\n") + 1
					open := strings.Index(text[idx:], "{") + idx
					end, err2 := braceBlockEnd(text, open)
					if err2 != nil {
						text = strings.ReplaceAll(text, "allInitScripts(", "allInitScripts_removed(")
					} else {
						// Also drop the signature line preceding the brace.
						text = text[:lineStart] + text[end:]
					}
				}
			}
			if !strings.Contains(text, "allBindings()") {
				text, err = appendToClassEnd(text, "class Page",
					"\tallBindings() {"+MustBody("patchPage_setBodyText_02")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// PageBinding class: whole-class replacement.
			mustContain(text, "class PageBinding", "PageBinding class")
			classIdx := findClassDecl(text, "PageBinding")
			if classIdx < 0 {
				return fmt.Errorf("PageBinding class decl not found")
			}
			classOpen := strings.Index(text[classIdx:], "{") + classIdx
			classEnd, err := braceBlockEnd(text, classOpen)
			if err != nil {
				return fmt.Errorf("PageBinding class: %w", err)
			}
			text = text[:classIdx] + strings.TrimSpace(MustBody("patchPage_replaceWithText_01")) + text[classEnd:]
			// InitScript ctor source simplification.
			text = strings.ReplaceAll(text,
				"this.source = `(() => {",
				"this.source = `(() => { ${source} })();`; // patchright (original below, removed)")
			if strings.Contains(text, "this.source = `(() => { ${source} })();`; // patchright") {
				// Remove the now-dead original assignment continuation is version-specific;
				// normalize to exactly the patchright form.
				if updated, err := regexpReplace(text,
					"this\\.source = `\\(\\(\\) => \\{ [^`]*?\\}`;",
					"this.source = `(() => { ${source} })();`;"); err == nil {
					text = updated
				}
			} else {
				mustContain(text, "this.source = `(() => {", "InitScript ctor")
				text, err = regexpReplace(text,
					"this\\.source = `\\(\\(\\) => \\{[\\s\\S]*?`;",
					"this.source = `(() => { ${source} })();`;")
				if err != nil {
					return fmt.Errorf("InitScript ctor: %w", err)
				}
			}
			// Worker.evaluateExpression(+Handle): isolatedContext selection.
			// Scoped to the Worker class via findClassDecl + paren-aware append.
			for _, method := range []string{"evaluateExpression", "evaluateExpressionHandle"} {
				if strings.Contains(text, "isolatedContext?: boolean") {
					break
				}
				wIdx := findClassDecl(text, "Worker")
				if wIdx < 0 {
					return fmt.Errorf("Worker class not found")
				}
				mIdx := findMethodDecl(text[wIdx:], method)
				if mIdx < 0 {
					return fmt.Errorf("Worker %s not found", method)
				}
				mIdx += wIdx
				pOpen := strings.Index(text[mIdx:], "(") + mIdx
				updated, err := appendParam(text, pOpen, "isolatedContext?: boolean")
				if err != nil {
					return fmt.Errorf("Worker %s param: %w", method, err)
				}
				text = updated
			}
			if !strings.Contains(text, "let context = await this._executionContextPromise;") {
				text = strings.Replace(text, "await this._executionContextPromise",
					strings.TrimSpace(MustBody("patchPage_insertStatements_02"))+"\n\t\t\tcontext // patchright", 1)
			}
			// _performLocatorHandlersCheckpoint: multi-frame early return.
			if strings.Contains(text, "await this.mainFrame().waitForSelector(progress, handler.selector, false, { state: 'hidden' });") {
				text = strings.ReplaceAll(text,
					"await this.mainFrame().waitForSelector(progress, handler.selector, false, { state: 'hidden' });",
					strings.TrimSpace(MustBody("patchPage_replaceWithText_02")))
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchUtilityScriptSerializers",
		Target: "packages/isomorphic/utilityScriptSerializers.ts",
		Apply: func(fs *FileSet) error {
			candidates := []string{
				"packages/isomorphic/utilityScriptSerializers.ts",
				"packages/playwright-core/src/utils/isomorphic/utilityScriptSerializers.ts",
			}
			rel := ""
			for _, c := range candidates {
				if fs.Has(c) {
					rel = c
					break
				}
			}
			if rel == "" {
				return fmt.Errorf("utilityScriptSerializers.ts not found")
			}
			text := fs.MustGet(rel)
			// Remove kFunctionBindingPrefix + kBindingsControllerProperty.
			for _, name := range []string{"kFunctionBindingPrefix", "kBindingsControllerProperty"} {
				if strings.Contains(text, name) {
					var err error
					text, err = regexpReplace(text, `\s*(export\s+)?(const|let|var)\s+`+name+`[^;]*;`, "")
					if err != nil {
						return err
					}
				}
			}
			// parseEvaluationResultValue: functionRegistry param.
			// v1.60 declares defaults (`handles: any[] = []`); use paren-aware append.
			if !strings.Contains(text, "functionRegistry") {
				declIdx := strings.Index(text, "function parseEvaluationResultValue")
				if declIdx < 0 {
					return fmt.Errorf("parseEvaluationResultValue not found")
				}
				open := strings.Index(text[declIdx:], "(") + declIdx
				updated, err := appendParam(text, open, "functionRegistry?: (name: string, callback: Function) => void")
				if err != nil {
					return err
				}
				text = updated
			}
			// fn bridge: older upstream carries `if ('fn' in value)`; v1.60
			// moved function bridging out of the isomorphic serializer, so
			// insert the patchright branch ahead of the value-type dispatch.
			if !strings.Contains(text, "callbackBridge") {
				if strings.Contains(text, "'fn' in value") || strings.Contains(text, `"fn" in value`) {
					anchor := "if ('fn' in value)"
					if !strings.Contains(text, anchor) {
						anchor = `if ("fn" in value)`
					}
					ifIdx := strings.Index(text, anchor)
					lineStart := strings.LastIndex(text[:ifIdx], "\n") + 1
					open := strings.Index(text[ifIdx:], "{") + ifIdx
					end, err := braceBlockEnd(text, open)
					if err != nil {
						return fmt.Errorf("fn bridge: %w", err)
					}
					text = text[:lineStart] + strings.TrimSpace(MustBody("patchUtilityScriptSerializers_replaceWithText_01")) + text[end:]
				} else {
					// v1.60 shape: splice the bridge ahead of the SerializedValue dispatch.
					mustContain(text, "if ('v' in value)", "value dispatch")
					text = strings.Replace(text, "if ('v' in value)",
						strings.TrimSpace(MustBody("patchUtilityScriptSerializers_replaceWithText_01"))+"\n    if ('v' in value)", 1)
				}
			}
			// innerSerialize: f<32hex> gate (older upstream only; v1.60 has no
			// function branch, so there is nothing to gate — skip cleanly).
			if strings.Contains(text, "value.name.startsWith") {
				var err error
				text, err = regexpReplace(text,
					`typeof value === 'function' && [^)]+?value\.name\.startsWith[^)]+\)`,
					"typeof value === 'function' && /^f[0-9a-f]{32}$/.test(value.name)")
				if err != nil {
					return fmt.Errorf("innerSerialize gate: %w", err)
				}
			}
			fs.Set(rel, text)
			// oldUtilityScriptSerializers.ts: new file.
			fs.Create("packages/playwright-core/src/utils/isomorphic/oldUtilityScriptSerializers.ts",
				MustBody("patchUtilityScriptSerializers_createSourceFile_01"))
			return nil
		},
	})
}
