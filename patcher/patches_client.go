package patcher

import (
	"fmt"
	"strings"
)

// This file ports patchright-nodejs/client_patches/ (10 files) plus the
// types.d.ts isolatedContext declarations from patchright_rebranding.ts.
// Client patches add the `isolatedContext` parameter (default true) to
// evaluate APIs and install the route-based init-script injection.

// addIsolatedParam adds `isolatedContext: boolean = true` to the method's
// parameter list and bumps assertMaxArguments counts inside it. Method lookup
// is generic-aware (`async evaluate<R, Arg>(`).
func addIsolatedParam(text, method string) (string, error) {
	idx := findMethodDecl(text, method)
	if idx < 0 {
		return "", fmt.Errorf("method %s not found", method)
	}
	open := strings.Index(text[idx:], "(") + idx
	end, err := parenEnd(text, open)
	if err != nil {
		return "", fmt.Errorf("method %s params: %w", method, err)
	}
	params := text[open+1 : end]
	if strings.Contains(params, "isolatedContext") {
		return text, nil
	}
	newParams := strings.TrimRight(params, " \t\n") + ", isolatedContext: boolean = true"
	text = text[:open+1] + newParams + text[end:]
	// Bump assertMaxArguments numeric literal inside the method body.
	mOpen := strings.Index(text[idx:], "{")
	if mOpen < 0 {
		return text, nil
	}
	mOpen += idx
	mEnd, err := braceBlockEnd(text, mOpen)
	if err != nil {
		return text, nil
	}
	body := text[mOpen:mEnd]
	re2 := mustCompile(`assertMaxArguments\((\d+),\s*(\d+)\)`)
	body = re2.ReplaceAllStringFunc(body, func(m string) string {
		sub := re2.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		n := 0
		for _, c := range sub[2] {
			n = n*10 + int(c-'0')
		}
		return "assertMaxArguments(" + sub[1] + ", " + itoa(n+1) + ")"
	})
	return text[:mOpen] + body + text[mEnd:], nil
}

func init() {
	register(PatchFunc{
		Name:   "patchClientBrowserContext",
		Target: "packages/playwright-core/src/client/browserContext.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/browserContext.ts"
			text := fs.MustGet(rel)
			var err error
			if !strings.Contains(text, "routeInjecting") {
				text, err = regexpReplace(text, `(export class BrowserContext[^{]*\{)`,
					"$1\n\trouteInjecting: boolean = false;")
				if err != nil {
					return err
				}
			}
			for _, m := range []string{"addInitScript", "exposeBinding", "exposeFunction"} {
				if !strings.Contains(text, m+"(") {
					return fmt.Errorf("client BrowserContext.%s not found", m)
				}
				anchor := "async " + m + "("
				idx := strings.Index(text, anchor)
				if idx < 0 {
					idx = strings.Index(text, m+"(")
				}
				open := strings.Index(text[idx:], "{") + idx
				if !strings.Contains(text[open:open+200], "installInjectRoute()") {
					text = text[:open+1] + "\n\t\tawait this.installInjectRoute();" + text[open+1:]
				}
			}
			if !strings.Contains(text, "async installInjectRoute(") {
				text, err = appendToClassEnd(text, "class BrowserContext",
					"\tasync installInjectRoute() {"+MustBody("patchBrowserContext_setBodyText_01")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// Dialog auto-dismiss trace suppression.
			text = strings.ReplaceAll(text,
				"dialog.accept({}, kNoTimeout).catch(() => {});",
				"dialogObject._wrapApiCall(() => dialog.accept({}, kNoTimeout).catch(() => {}), { internal: true });")
			text = strings.ReplaceAll(text,
				"dialog.dismiss({}, kNoTimeout).catch(() => {});",
				"dialogObject._wrapApiCall(() => dialog.dismiss({}, kNoTimeout).catch(() => {}), { internal: true });")
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientHelper",
		Target: "packages/playwright-core/src/client/clientHelper.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/clientHelper.ts"
			text := fs.MustGet(rel)
			// Drop stealth-hostile serializer imports. Either name may lead
			// the import list (v1.62: kBindingsControllerProperty first), so
			// try comma-first then bare, tolerating absence after the other
			// removal already took the comma with it.
			for _, name := range []string{"kBindingsControllerProperty", "kFunctionBindingPrefix"} {
				if !strings.Contains(text, name) {
					continue
				}
				if updated, err := regexpReplace(text, `,\s*`+name+`\b`, ""); err == nil {
					text = updated
					continue
				}
				if updated, err := regexpReplace(text, `\b`+name+`\s*,?`, ""); err == nil {
					text = updated
					continue
				}
				return fmt.Errorf("cannot remove import %s", name)
			}
			if !strings.Contains(text, "rawUtilityScriptSource") {
				text = "import * as rawUtilityScriptSource from \"../generated/utilityScriptSource\";\n" + text
			}
			// initScriptSourceWithExposedFunctions (older upstream; removed in
			// v1.60 where callback serialization moved to protocol/serializers).
			if strings.Contains(text, "initScriptSourceWithExposedFunctions") {
				updated, err := replaceMethodBody(text, "initScriptSourceWithExposedFunctions(",
					MustBody("patchClientHelper_setBodyText_01"))
				if err != nil {
					fIdx := strings.Index(text, "initScriptSourceWithExposedFunctions")
					fOpen := strings.Index(text[fIdx:], "{") + fIdx
					fEnd, err2 := braceBlockEnd(text, fOpen)
					if err2 != nil {
						return fmt.Errorf("initScriptSourceWithExposedFunctions: %v / %v", err, err2)
					}
					updated = text[:fOpen+1] + MustBody("patchClientHelper_setBodyText_01") + text[fEnd-1:]
				}
				text = updated
			}
			// addSourceUrlToScript: identity (no sourceURL wrapping -> stealth).
			if strings.Contains(text, "addSourceUrlToScript") && !strings.Contains(text, "return source // patchright") {
				fIdx := strings.Index(text, "addSourceUrlToScript")
				fOpen := strings.Index(text[fIdx:], "{") + fIdx
				fEnd, err := braceBlockEnd(text, fOpen)
				if err != nil {
					return fmt.Errorf("addSourceUrlToScript: %w", err)
				}
				text = text[:fOpen+1] + "\n\treturn source; // patchright" + text[fEnd-1:]
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientClock",
		Target: "packages/playwright-core/src/client/clock.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/clock.ts"
			text := fs.MustGet(rel)
			mustContain(text, "async install(", "client Clock.install")
			if !strings.Contains(text, "installInjectRoute()") {
				iIdx := strings.Index(text, "install(")
				iOpen := strings.Index(text[iIdx:], "{") + iIdx
				text = text[:iOpen+1] + "\n\t\tawait this._browserContext.installInjectRoute();" + text[iOpen+1:]
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientFrame",
		Target: "packages/playwright-core/src/client/frame.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/frame.ts"
			text := fs.MustGet(rel)
			var err error
			text, err = replaceMethodBody(text, "waitForURL(",
				MustBody("patchFrame_setBodyText_01"))
			if err != nil {
				return fmt.Errorf("client waitForURL: %w", err)
			}
			for _, m := range []struct{ method, channel string }{
				{"evaluate", "this._channel.evaluateExpression"},
				{"evaluateHandle", "this._channel.evaluateExpression"},
				{"$$eval", "this._channel.evalOnSelectorAll"},
			} {
				text, err = addIsolatedParam(text, m.method)
				if err != nil {
					return fmt.Errorf("client frame %s: %w", m.method, err)
				}
				// Best effort: the isolatedContext param is the load-bearing
				// half; skip when this variant's channel shape differs.
				if updated, err := addChannelProp(text, m.channel); err == nil {
					text = updated
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientJsHandle",
		Target: "packages/playwright-core/src/client/jsHandle.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/jsHandle.ts"
			text := fs.MustGet(rel)
			for _, m := range []struct{ method, channel string }{
				{"evaluate", "this._channel.evaluateExpression"},
				{"evaluateHandle", "this._channel.evaluateExpressionHandle"},
			} {
				// NOTE: no assertMaxArguments bump upstream for jsHandle; add param + channel prop.
				idx := findMethodDecl(text, m.method)
				if idx < 0 {
					return fmt.Errorf("client jsHandle %s not found", m.method)
				}
				open := strings.Index(text[idx:], "(") + idx
				end, err := parenEnd(text, open)
				if err != nil {
					return fmt.Errorf("client jsHandle %s params: %w", m.method, err)
				}
				if !strings.Contains(text[open:end], "isolatedContext") {
					text = text[:end] + ", isolatedContext: boolean = true" + text[end:]
				}
				if updated, err := addChannelProp(text, m.channel); err == nil {
					text = updated
				}
			}
			// kFunctionBindingPrefix removal + f<guid> callback names. The name
			// may lead the import list (v1.62: solo import) so try comma-first
			// then bare, tolerating absence.
			if strings.Contains(text, "kFunctionBindingPrefix") {
				if updated, err := regexpReplace(text, `,\s*kFunctionBindingPrefix\b`, ""); err == nil {
					text = updated
				} else if updated, err := regexpReplace(text, `\bkFunctionBindingPrefix\s*,?`, ""); err == nil {
					text = updated
				} else {
					return fmt.Errorf("cannot remove kFunctionBindingPrefix import")
				}
			}
			if strings.Contains(text, "serializeArgumentWithCallbacks") {
				mustContain(text, "const name =", "serializeArgumentWithCallbacks")
				var err error
				text, err = regexpReplace(text,
					`(const name\s*=\s*)[^;]+;`,
					"$1'f' + createGuid();")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientLocator",
		Target: "packages/playwright-core/src/client/locator.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/locator.ts"
			text := fs.MustGet(rel)
			for _, m := range []struct{ method, call string }{
				{"evaluate", "h.evaluate"},
				{"evaluateHandle", "h.evaluateHandle"},
				{"evaluateAll", "this._frame.$$eval"},
			} {
				idx := findMethodDecl(text, m.method)
				if idx < 0 {
					return fmt.Errorf("client locator %s not found", m.method)
				}
				open := strings.Index(text[idx:], "(") + idx
				end, err := parenEnd(text, open)
				if err != nil {
					return fmt.Errorf("client locator %s params: %w", m.method, err)
				}
				if !strings.Contains(text[open:end], "isolatedContext") {
					text = text[:end] + ", isolatedContext: boolean = true" + text[end:]
				}
				// Append isolatedContext to the inner call.
				callIdx := strings.Index(text, m.call+"(")
				if callIdx < 0 {
					return fmt.Errorf("client locator inner call %s not found", m.call)
				}
				cOpen := strings.Index(text[callIdx:], "(") + callIdx
				cEnd, err := parenEnd(text, cOpen)
				if err != nil {
					return fmt.Errorf("client locator inner call %s: %w", m.call, err)
				}
				if !strings.Contains(text[cOpen:cEnd], "isolatedContext") {
					text = text[:cEnd] + ", isolatedContext" + text[cEnd:]
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientNetwork",
		Target: "packages/playwright-core/src/client/network.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/network.ts"
			text := fs.MustGet(rel)
			var err error
			// TargetClosedError import (single or double quotes upstream).
			if !strings.Contains(text, "TargetClosedError") {
				text, err = regexpReplace(text, `(from ['"]\./errors['"][^;]*;)`, "$1\nimport { TargetClosedError } from './errors';")
				if err != nil {
					text = "import { TargetClosedError } from './errors';\n" + text
				}
			}
			text, err = replaceMethodBody(text, "allHeaders(",
				MustBody("patchNetwork_setBodyText_03"))
			if err != nil {
				return fmt.Errorf("client allHeaders: %w", err)
			}
			text, err = replaceMethodBody(text, "_applyFallbackOverrides(",
				MustBody("patchNetwork_setBodyText_04"))
			if err != nil {
				return fmt.Errorf("client fallback overrides: %w", err)
			}
			if !strings.Contains(text, "patchrightInitScript") {
				updated, err := regexpReplace(text,
					`(this\._channel\.continue\(\{[^}]*?)(\}\))`,
					"$1, patchrightInitScript: (options as any).patchrightInitScript$2")
				if err != nil {
					updated, err = regexpReplace(text,
						`(this\._channel\.continue\(\{[\s\S]*?)(}, kNoTimeout\))`,
						"$1, patchrightInitScript: (options as any).patchrightInitScript$2")
				}
				if err != nil {
					return err
				}
				text = updated
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientPage",
		Target: "packages/playwright-core/src/client/page.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/page.ts"
			text := fs.MustGet(rel)
			var err error
			if !strings.Contains(text, "routeInjecting") {
				text, err = regexpReplace(text, `(export class Page[^{]*\{)`,
					"$1\n\trouteInjecting: boolean = false;")
				if err != nil {
					return err
				}
			}
			for _, m := range []string{"addInitScript", "exposeBinding", "exposeFunction"} {
				anchor := "async " + m + "("
				idx := strings.Index(text, anchor)
				if idx < 0 {
					idx = strings.Index(text, m+"(")
				}
				if idx < 0 {
					return fmt.Errorf("client Page.%s not found", m)
				}
				open := strings.Index(text[idx:], "{") + idx
				if !strings.Contains(text[open:open+200], "installInjectRoute()") {
					text = text[:open+1] + "\n\t\tawait this.installInjectRoute();" + text[open+1:]
				}
			}
			if !strings.Contains(text, "async installInjectRoute(") {
				text, err = appendToClassEnd(text, "class Page",
					"\tasync installInjectRoute() {"+MustBody("patchPage_setBodyText_03")+"\n\t}")
				if err != nil {
					return err
				}
			}
			for _, m := range []string{"evaluate", "evaluateHandle"} {
				text, err = addIsolatedParam(text, m)
				if err != nil {
					return fmt.Errorf("client page %s: %w", m, err)
				}
				call := "this._mainFrame." + mapMethod(m)
				callIdx := strings.Index(text, call+"(")
				if callIdx >= 0 {
					cOpen := strings.Index(text[callIdx:], "(") + callIdx
					if cEnd, err := parenEnd(text, cOpen); err == nil {
						if !strings.Contains(text[cOpen:cEnd], "isolatedContext") {
							text = text[:cEnd] + ", isolatedContext" + text[cEnd:]
						}
					}
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientTracing",
		Target: "packages/playwright-core/src/client/tracing.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/tracing.ts"
			text := fs.MustGet(rel)
			if !strings.Contains(text, "installInjectRoute") {
				sIdx := strings.Index(text, "async start(")
				if sIdx < 0 {
					sIdx = strings.Index(text, "start(")
				}
				if sIdx < 0 {
					return fmt.Errorf("client Tracing.start not found")
				}
				sOpen := strings.Index(text[sIdx:], "{") + sIdx
				text = text[:sOpen+1] + "\n\t\tif (typeof this._parent.installInjectRoute === 'function') await this._parent.installInjectRoute();" + text[sOpen+1:]
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientWorker",
		Target: "packages/playwright-core/src/client/worker.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/client/worker.ts"
			text := fs.MustGet(rel)
			var err error
			for _, m := range []string{"evaluate", "evaluateHandle"} {
				text, err = addIsolatedParam(text, m)
				if err != nil {
					return fmt.Errorf("client worker %s: %w", m, err)
				}
				if updated, err := addChannelProp(text, "this._channel.evaluateExpression"); err == nil {
					text = updated
				} else if updated, err := addChannelProp(text, "this._channel.evaluateExpressionHandle"); err == nil {
					text = updated
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchClientTypes",
		Target: "packages/playwright-core/types/types.d.ts",
		Apply: func(fs *FileSet) error {
			// Ports addIsolatedContextParameter from patchright_rebranding.ts:
			// append `isolatedContext?: boolean` to evaluate signatures in the
			// Page/Worker/Frame/Locator/JSHandle interfaces.
			const rel = "packages/playwright-core/types/types.d.ts"
			text := fs.MustGet(rel)
			text = addIsolatedContextToTypes(text)
			fs.Set(rel, text)
			return nil
		},
	})
}

func mapMethod(m string) string {
	if m == "evaluate" {
		return "evaluate"
	}
	return "evaluateHandle"
}

// addIsolatedContextToTypes appends `isolatedContext?: boolean` to evaluate
// signatures in the Page/Worker/Frame/Locator/JSHandle interfaces.
func addIsolatedContextToTypes(text string) string {
	lines := strings.Split(text, "\n")
	var currentIface string
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "interface ") || strings.HasPrefix(trim, "export interface ") {
			name := trim
			name = strings.TrimPrefix(name, "export interface ")
			name = strings.TrimPrefix(name, "interface ")
			if idx := strings.IndexAny(name, " <{"); idx >= 0 {
				name = name[:idx]
			}
			currentIface = strings.TrimSpace(name)
		}
		switch currentIface {
		case "Page", "Worker", "Frame", "Locator", "JSHandle":
		default:
			continue
		}
		for _, m := range []string{"evaluate", "evaluateHandle", "evaluateAll"} {
			if strings.HasPrefix(trim, m+"(") || strings.HasPrefix(trim, m+"<") {
				if strings.Contains(line, "isolatedContext") {
					continue
				}
				// Member declarations end with `;` (e.g. `): Promise<R>;` or
				// `, options?: X): Promise<R>;`); insert before the closing paren.
				if idx := strings.LastIndex(line, "):"); idx >= 0 {
					lines[i] = line[:idx] + ", isolatedContext?: boolean" + line[idx:]
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}
