package patcher

import (
	"fmt"
	"strings"
)

// This file ports patchCRPage (server/chromium/crPage.ts): Runtime.enable
// avoidance, init-script registry for route injection, binding stealth,
// injected-node cleanup, and utility-world bootstrap.

func init() {
	register(PatchFunc{
		Name:   "patchCRPage",
		Target: "packages/playwright-core/src/server/chromium/crPage.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crPage.ts"
			text := fs.MustGet(rel)
			var err error

			if !strings.Contains(text, `from "crypto"`) && !strings.Contains(text, `from 'crypto'`) {
				text, err = regexpReplace(text, `(import[^\n]*\n)`, "$1import crypto from \"crypto\";\n")
				if err != nil {
					return err
				}
			}
			// CRPage: _reportAsNewPromise + _reportAsNew.
			if !strings.Contains(text, "_reportAsNewPromise") {
				mustContain(text, "class CRPage", "CRPage class")
				text, err = regexpReplace(text, `(class CRPage[^{]*\{)`,
					"$1\n\tprivate _reportAsNewPromise: Promise<void> | undefined;")
				if err != nil {
					return err
				}
				text, err = appendToClassEnd(text, "class CRPage",
					"\t_reportAsNew(error?: Error): Promise<void> {\n\t\treturn this._reportAsNewPromise ??= this._page.reportAsNew(this._opener?._page, error);\n\t}")
				if err != nil {
					return err
				}
			}
			// Ctor: always-on interception + unique script tag.
			oldCtor := "this.updateRequestInterception();"
			mustContain(text, oldCtor, "CRPage ctor interception")
			text = strings.Replace(text, oldCtor,
				strings.TrimSpace(MustBody("patchCRPage_replaceWithText_01")), 1)
			text = strings.ReplaceAll(text,
				"this._page.reportAsNew(this._opener?._page, undefined)", "this._reportAsNew()")
			text = strings.ReplaceAll(text,
				"this._page.reportAsNew(this._opener?._page, error)", "this._reportAsNew(error)")
			// exposeBinding / removeExposedBindings methods.
			if !strings.Contains(text, "async exposeBinding(binding") {
				text, err = appendToClassEnd(text, "class CRPage",
					"\tasync exposeBinding(binding: PageBinding) {"+MustBody("patchCRPage_setBodyText_01")+"\n\t}"+
						"\n\tasync removeExposedBindings() {"+MustBody("patchCRPage_setBodyText_02")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// addInitScript: register into page.initScripts.
			mustContain(text, "addInitScript(", "CRPage addInitScript")
			if !strings.Contains(text, "this._page.initScripts.push(initScript);") {
				aIdx := strings.Index(text, "addInitScript(")
				aOpen := strings.Index(text[aIdx:], "{") + aIdx
				text = text[:aOpen+1] + "\n\t\tthis._page.initScripts.push(initScript);" + text[aOpen+1:]
			}
			// _sessionForFrame message.
			text = strings.ReplaceAll(text, "Frame has been detached.", "Frame was detached")
			// FrameSession properties.
			for _, prop := range []string{
				"\tprivate _exposedBindingNames: string[] = [];",
				"\tprivate _evaluateOnNewDocumentScripts: InitScript[] = [];",
				"\tprivate _parsedExecutionContextIds: number[] = [];",
				"\tprivate _exposedBindingScripts: string[] = [];",
			} {
				name := strings.Split(strings.TrimSpace(prop), ":")[0]
				name = strings.TrimPrefix(strings.TrimSpace(name), "private ")
				if !strings.Contains(text, name) {
					text, err = regexpReplace(text, `(class FrameSession[^{]*\{)`, "$1\n"+prop)
					if err != nil {
						return err
					}
				}
			}
			// _initialize head: early Page.enable + dialog listener.
			if !strings.Contains(text, "const pageEnablePromise = this._client.send('Page.enable');") {
				iIdx := strings.Index(text, "async _initialize(")
				if iIdx < 0 {
					return fmt.Errorf("FrameSession _initialize not found")
				}
				iOpen := strings.Index(text[iIdx:], "{") + iIdx
				text = text[:iOpen+1] + "\n" + MustBody("patchCRPage_insertStatements_01") + text[iOpen+1:]
			}
			// Opener-resume gate.
			if !strings.Contains(text, "if (this._isMainFrame() && !this._crPage._opener)") {
				old := "promises.push(this._client.send('Runtime.runIfWaitingForDebugger'));"
				mustContain(text, old, "resume target")
				text = strings.Replace(text, old, old+"\n"+
					strings.TrimSpace(MustBody("patchCRPage_insertStatements_02")), 1)
			}
			// promises[]: drop Runtime.enable + kPlaywrightBinding; reuse early Page.enable.
			text = strings.ReplaceAll(text, "\n\t\t\tthis._client.send('Runtime.enable'),", "")
			text = strings.ReplaceAll(text, "\n\t\t\tthis._client.send('Runtime.enable')", "")
			lines := strings.Split(text, "\n")
			kept := lines[:0]
			for _, line := range lines {
				if strings.Contains(line, "Runtime.enable") && strings.Contains(line, "this._client.send") {
					continue
				}
				if strings.Contains(line, "Runtime.addBinding") && strings.Contains(line, "kPlaywrightBinding") {
					continue
				}
				kept = append(kept, line)
			}
			text = strings.Join(kept, "\n")
			// Reuse early Page.enable promise.
			if strings.Contains(text, "this._client.send('Page.enable')") {
				text = strings.ReplaceAll(text, "this._client.send('Page.enable')", "pageEnablePromise")
			}
			// Post-getFrameTree: stop dialog listener, drop isolated-world bootstrap.
			if !strings.Contains(text, "initializingDialogs = false;") {
				mustContain(text, "this._addRendererListeners()", "renderer listeners")
				text = strings.Replace(text, "this._addRendererListeners()",
					"this._addRendererListeners();\n\t\t\tinitializingDialogs = false;", 1)
				// Remove localFrames + createIsolatedWorld bootstrap statements.
				out := []string{}
				for _, line := range strings.Split(text, "\n") {
					if strings.Contains(line, "const localFrames = this._isMainFrame() ? this._page.frames()") ||
						strings.Contains(line, "Page.createIsolatedWorld") {
						continue
					}
					out = append(out, line)
				}
				text = strings.Join(out, "\n")
			}
			// Non-initial navigation branch: lazy utility contexts + bindings.
			if !strings.Contains(text, `this._page.frameManager.frame(frame._id)._context("utility")`) {
				mustContain(text, "if (isInitialEmptyPage)", "initial page branch")
				text = strings.Replace(text, "} else {",
					"} else {"+MustBody("patchCRPage_insertStatements_03"), 1)
			}
			// focusControl gate.
			if strings.Contains(text, "Emulation.setFocusEmulationEnabled") && !strings.Contains(text, "!this._crPage._browserContext._options.focusControl") {
				if updated, err := regexpReplace(text,
					`if\s*\(this\._isMainFrame\(\)[^)]*Emulation\.setFocusEmulationEnabled[\s\S]*?\)\);`,
					strings.TrimSpace(MustBody("patchCRPage_replaceWithText_02"))); err == nil {
					text = updated
				} else {
					// Fall back: replace the whole if statement loosely.
					old := "promises.push(this._client.send(\"Emulation.setFocusEmulationEnabled\", { enabled: true }));"
					if strings.Contains(text, old) {
						text = strings.Replace(text, old,
							strings.TrimSpace(MustBody("patchCRPage_replaceWithText_02")), 1)
					}
				}
			}
			// Init-script evaluation loops -> binding/initScript registry loops.
			if strings.Contains(text, "this._crPage._page.allInitScripts()") {
				text = strings.ReplaceAll(text,
					"for (const initScript of this._crPage._page.allInitScripts())",
					"for (const initScript of [...this._crPage._browserContext.initScripts, ...this._crPage._page.initScripts]) // patchright")
				if strings.Contains(text, "frame.evaluateExpression(initScript.source)") {
					text = strings.ReplaceAll(text,
						"frame.evaluateExpression(initScript.source)",
						"frame.evaluateExpression(initScript.source) // patchright: see registry loops below")
					// Replace the for-of bodies carrying those calls (best effort).
					if updated, err := regexpReplace(text,
						`for\s*\([^{]*allInitScripts\(\)\)[^{]*\{[\s\S]*?frame\.evaluateExpression\(initScript\.source\)[^}]*\}`,
						strings.TrimSpace(MustBody("patchCRPage_replaceWithText_03"))); err == nil {
						text = updated
					}
				}
				if strings.Contains(text, "promises.push(this._evaluateOnNewDocument(") {
					if updated, err := regexpReplace(text,
						`for\s*\([^{]*allInitScripts\(\)\)[^{]*\{[\s\S]*?promises\.push\(this\._evaluateOnNewDocument\([^)]*\)\)[^}]*\}`,
						strings.TrimSpace(MustBody("patchCRPage_replaceWithText_04"))); err == nil {
						text = updated
					}
				}
			}
			// _onDialog head + detached-frame condition.
			if !strings.Contains(text, "this._firstNonInitialNavigationCommittedFulfill();\n\t\tif (this._isMainFrame()") {
				dIdx := strings.Index(text, "async _onDialog(")
				if dIdx < 0 {
					dIdx = strings.Index(text, "_onDialog(")
				}
				if dIdx >= 0 {
					dOpen := strings.Index(text[dIdx:], "{") + dIdx
					text = text[:dOpen+1] + "\n" + MustBody("patchCRPage_insertStatements_04") + text[dOpen+1:]
				}
			}
			text = strings.ReplaceAll(text,
				"!this._page.frameManager.frame(this._targetId)",
				"!this._page.frameManager.frame(this._targetId) && !this._isMainFrame()")
			// _initBinding / _removeExposedBindings.
			if !strings.Contains(text, "async _initBinding(") {
				text, err = appendToClassEnd(text, "class FrameSession",
					"\tasync _initBinding(binding: PageBinding) {"+MustBody("patchCRPage_setBodyText_03")+"\n\t}"+
						"\n\tasync _removeExposedBindings() {"+MustBody("patchCRPage_setBodyText_04")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// _onLifecycleEvent tail: load-only init-script cleanup.
			if !strings.Contains(text, `event.name !== "load"`) {
				lIdx := strings.Index(text, "async _onLifecycleEvent(")
				if lIdx < 0 {
					lIdx = strings.Index(text, "_onLifecycleEvent(")
				}
				if lIdx < 0 {
					return fmt.Errorf("_onLifecycleEvent not found")
				}
				lOpen := strings.Index(text[lIdx:], "{") + lIdx
				lEnd, err := braceBlockEnd(text, lOpen)
				if err != nil {
					return err
				}
				text = text[:lEnd-1] + MustBody("patchCRPage_addStatements_02") + text[lEnd-1:]
			}
			// _eventBelongsToStaleFrame / _onFrameNavigated / exec-context hooks.
			text, err = replaceMethodBody(text, "_eventBelongsToStaleFrame(",
				MustBody("patchCRPage_setBodyText_05"))
			if err != nil {
				return fmt.Errorf("_eventBelongsToStaleFrame: %w", err)
			}
			if !strings.Contains(text, "binding.dispatchFunction(this._page, context)") {
				nIdx := strings.Index(text, "async _onFrameNavigated(")
				if nIdx < 0 {
					nIdx = strings.Index(text, "_onFrameNavigated(")
				}
				if nIdx < 0 {
					return fmt.Errorf("_onFrameNavigated not found")
				}
				nOpen := strings.Index(text[nIdx:], "{") + nIdx
				nEnd, err := braceBlockEnd(text, nOpen)
				if err != nil {
					return err
				}
				text = text[:nEnd-1] + MustBody("patchCRPage_addStatements_03") + text[nEnd-1:]
			}
			if !strings.Contains(text, "'Runtime.addBinding', { name: name, executionContextId") {
				eIdx := strings.Index(text, "async _onExecutionContextCreated(")
				if eIdx < 0 {
					eIdx = strings.Index(text, "_onExecutionContextCreated(")
				}
				if eIdx < 0 {
					return fmt.Errorf("_onExecutionContextCreated not found")
				}
				eOpen := strings.Index(text[eIdx:], "{") + eIdx
				text = text[:eOpen+1] + "\n" + MustBody("patchCRPage_insertStatements_05") + text[eOpen+1:]
			}
			if !strings.Contains(text, `contextPayload.auxData?.type === "worker"`) {
				text = strings.Replace(text,
					"for (const name of this._exposedBindingNames)",
					strings.TrimSpace(MustBody("patchCRPage_insertStatements_06"))+"\n\t\tfor (const name of this._exposedBindingNames)", 1)
			}
			// worldName branching -> direct assignment + main|utility guard.
			if strings.Contains(text, "let worldName: types.World|null") {
				text = strings.ReplaceAll(text, "let worldName: types.World|null", "let worldName = contextPayload.name; // patchright")
				for _, stmt := range []string{
					"worldName = 'main'", "worldName = 'utility'",
					"else if (contextPayload.name === this._crPage.utilityWorldName)",
				} {
					for strings.Contains(text, stmt) {
						var err error
						text, err = removeLineContaining(text, stmt)
						if err != nil {
							break
						}
					}
				}
				text = strings.ReplaceAll(text, "if (worldName)",
					strings.TrimSpace(MustBody("patchCRPage_replaceWithText_05")))
			}
			if !strings.Contains(text, "for (const source of this._exposedBindingScripts)") {
				eIdx := strings.Index(text, "_onExecutionContextCreated(")
				eOpen := strings.Index(text[eIdx:], "{") + eIdx
				eEnd, err := braceBlockEnd(text, eOpen)
				if err != nil {
					return err
				}
				text = text[:eEnd-1] + MustBody("patchCRPage_addStatements_04") + text[eEnd-1:]
			}
			// _onAttachedToTarget: drop Runtime.enable, add globalThis bootstrap.
			if strings.Contains(text, "session._sendMayFail('Runtime.enable');") {
				var err error
				text, err = removeLineContaining(text, "session._sendMayFail('Runtime.enable');")
				if err != nil {
					return err
				}
			}
			if !strings.Contains(text, "worker.createExecutionContext(new CRExecutionContext(session") {
				mustContain(text, "session.once('Runtime.executionContextCreated'", "attached session once")
				text = strings.Replace(text, "session.once('Runtime.executionContextCreated'",
					strings.TrimSpace(MustBody("patchCRPage_insertStatements_07"))+"\n\t\t\tsession.once('Runtime.executionContextCreated'", 1)
			}
			// _onBindingCalled fallback (v1.60 nests it in a pageOrError
			// check with a braceless if; older upstream braces it).
			if !strings.Contains(text, "fallbackContext") {
				updated, err := regexpReplace(text,
					`if\s*\(context\)\s*\{\s*await this\._page\.onBindingCalled\(event\.payload,\s*context\);\s*\}`,
					strings.TrimSpace(MustBody("patchCRPage_replaceWithText_06")))
				if err != nil {
					updated, err = regexpReplace(text,
						`if\s*\(context\)\s*\n?\s*await this\._page\.onBindingCalled\(event\.payload,\s*context\);`,
						strings.TrimSpace(MustBody("patchCRPage_replaceWithText_06")))
				}
				if err != nil {
					return fmt.Errorf("_onBindingCalled: %w", err)
				}
				text = updated
			}
			// _evaluateOnNewDocument / _removeEvaluatesOnNewDocument.
			text, err = replaceMethodBody(text, "_evaluateOnNewDocument(",
				MustBody("patchCRPage_setBodyText_06"))
			if err != nil {
				return fmt.Errorf("_evaluateOnNewDocument: %w", err)
			}
			text, err = replaceMethodBody(text, "_removeEvaluatesOnNewDocument(",
				MustBody("patchCRPage_setBodyText_07"))
			if err != nil {
				return fmt.Errorf("_removeEvaluatesOnNewDocument: %w", err)
			}
			// _adoptBackendNodeId: direct delegate access.
			text = strings.ReplaceAll(text,
				"executionContextId: (to.delegate as CRExecutionContext)._contextId",
				"executionContextId: to.delegate._contextId")
			fs.Set(rel, text)
			return nil
		},
	})
}
