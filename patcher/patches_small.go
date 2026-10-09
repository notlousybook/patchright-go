package patcher

import (
	"fmt"
	"regexp"
	"strings"
)

// This file ports the self-contained driver patches that operate with pure
// string transforms: chromiumSwitchesPatch, chromiumPatch, credentialsPatch,
// launchAppPatch, buildPatch, recorderPatch, cliAliasPatch, crDevToolsPatch,
// crCoveragePatch, browserContextDispatcherPatch, networkPatch,
// networkDispatchersPatch, jsHandleDispatcherPatch, pageDispatcherPatch.

const chromiumSwitchesFile = "packages/playwright-core/src/server/chromium/chromiumSwitches.ts"

// SwitchesToDisable mirrors switchesToDisable in chromiumSwitchesPatch.ts.
var SwitchesToDisable = []string{
	"assistantMode ? '' : '--enable-automation'",
	"'--disable-popup-blocking'",
	"'--disable-component-update'",
	"'--disable-default-apps'",
	"'--disable-extensions'",
	"'--disable-client-side-phishing-detection'",
	"'--disable-component-extensions-with-background-pages'",
	"'--allow-pre-commit-input'",
	"'--disable-ipc-flooding-protection'",
	"'--metrics-recording-only'",
	"'--unsafely-disable-devtools-self-xss-warnings'",
	"'--disable-back-forward-cache'",
	"'--disable-features=ImprovedCookieControls,LazyFrameLoading,GlobalMediaControls,DestroyProfileOnBrowserClose,MediaRouter,DialMediaRouteProvider,AcceptCHFrame,AutoExpandDetailsElement,CertificateTransparencyComponentUpdater,AvoidUnnecessaryBeforeUnloadCheckSync,Translate,HttpsUpgrades,PaintHolding,ThirdPartyStoragePartitioning,LensOverlay,PlzDedicatedWorker'",
}

// StealthSwitch mirrors the single added switch.
const StealthSwitch = "'--disable-blink-features=AutomationControlled'"

// bareFlag normalizes a switchesToDisable entry (a TS source fragment) to the
// bare runtime flag, e.g. `assistantMode ? ” : '--enable-automation'` ->
// `--enable-automation`.
func bareFlag(sw string) string {
	flag := sw
	if i := strings.Index(flag, "'--"); i >= 0 {
		flag = flag[i+1:]
		if j := strings.Index(flag, "'"); j >= 0 {
			flag = flag[:j]
		}
		return flag
	}
	if i := strings.Index(flag, `"--`); i >= 0 {
		flag = flag[i+1:]
		if j := strings.Index(flag, `"`); j >= 0 {
			flag = flag[:j]
		}
		return flag
	}
	if strings.HasPrefix(sw, "'--") && strings.HasSuffix(sw, "'") {
		return strings.Trim(sw, "'")
	}
	return ""
}

// FilterChromiumSwitches is the stealth/switches.go counterpart: it applies the
// same add/remove policy to a parsed switch list (used by the Go client when it
// cannot rely on the patched driver, and unit-testable without a checkout).
func FilterChromiumSwitches(switches []string) []string {
	disabled := map[string]bool{}
	for _, s := range SwitchesToDisable {
		// Entries are TS source fragments (e.g. `assistantMode ? '' :
		// '--enable-automation'`); normalize to the bare runtime flag.
		flag := s
		if i := strings.Index(flag, "'--"); i >= 0 {
			flag = flag[i+1:]
			if j := strings.Index(flag, "'"); j >= 0 {
				flag = flag[:j]
			}
		}
		disabled[strings.Trim(flag, "'")] = true
	}
	out := make([]string, 0, len(switches)+1)
	for _, s := range switches {
		if disabled[strings.Trim(s, "'")] {
			continue
		}
		out = append(out, s)
	}
	out = append(out, strings.Trim(StealthSwitch, "'"))
	return out
}

func init() {
	register(PatchFunc{
		Name:   "patchChromiumSwitches",
		Target: chromiumSwitchesFile,
		Apply: func(fs *FileSet) error {
			text := fs.MustGet(chromiumSwitchesFile)
			mustContain(text, "chromiumSwitches", "switch array")
			// Remove one source line per disabled flag. Matching is by bare
			// flag text so trailing comments and option ternaries
			// (assistantMode, options?.android) don't matter; the whole line
			// goes, mirroring removeElement in the TS patch.
			removed := map[string]bool{}
			lines := strings.Split(text, "\n")
			kept := make([]string, 0, len(lines))
			for _, line := range lines {
				drop := false
				for _, sw := range SwitchesToDisable {
					flag := bareFlag(sw)
					if flag == "" {
						continue
					}
					if strings.Contains(line, "'"+flag+"'") || strings.Contains(line, `"`+flag+`"`) {
						drop = true
						removed[sw] = true
						break
					}
				}
				if !drop {
					kept = append(kept, line)
				}
			}
			// The --disable-features mega-flag moved to a disabledFeatures
			// join in newer upstream; drop that line too when present.
			for i, line := range kept {
				if strings.Contains(line, "'--disable-features=' + disabledFeatures.join") {
					kept = append(kept[:i], kept[i+1:]...)
					removed["'--disable-features=...'"] = true
					break
				}
			}
			text = strings.Join(kept, "\n")
			// Append the stealth switch before the array close (`];` or
			// `].filter(Boolean);` in newer upstream).
			decl := strings.Index(text, "chromiumSwitches")
			if decl < 0 {
				return fmt.Errorf("chromiumSwitches not found")
			}
			tail := strings.LastIndex(text[decl:], "].filter(Boolean);")
			closeLen := len("].filter(Boolean);")
			if tail < 0 {
				tail = strings.LastIndex(text[decl:], "];")
				closeLen = len("];")
			}
			if tail < 0 {
				return fmt.Errorf("chromiumSwitches array close not found")
			}
			pos := decl + tail
			_ = closeLen
			text = text[:pos] + "\n  " + StealthSwitch + "," + text[pos:]
			fs.Set(chromiumSwitchesFile, text)
			_ = removed
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchChromium",
		Target: "packages/playwright-core/src/server/chromium/chromium.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/chromium.ts"
			text := fs.MustGet(rel)
			// Always use --headless=new (mirrors the IfStatement replacement).
			// Older upstream gated this behind PLAYWRIGHT_CHROMIUM_USE_HEADLESS_NEW;
			// v1.60 pushes '--headless' directly, so rewrite that push.
			if strings.Contains(text, "PLAYWRIGHT_CHROMIUM_USE_HEADLESS_NEW") {
				var err error
				text, err = regexpReplace(text,
					`if\s*\(process\.env\.PLAYWRIGHT_CHROMIUM_USE_HEADLESS_NEW[^)]*\)\s*\{[^}]*\}`,
					"chromeArguments.push('--headless=new');")
				if err != nil {
					text2, err2 := regexpReplace(fs.MustGet(rel),
						`if\s*\(process\.env\.PLAYWRIGHT_CHROMIUM_USE_HEADLESS_NEW[^)]*\)[^\n;]*;?`,
						"chromeArguments.push('--headless=new');")
					if err2 != nil {
						return err
					}
					text = text2
				}
			} else {
				mustContain(text, "chromeArguments.push('--headless')", "headless push")
				text = strings.ReplaceAll(text,
					"chromeArguments.push('--headless');",
					"chromeArguments.push('--headless=new');")
			}
			// Remove --enable-unsafe-swiftshader pushes.
			lines := strings.Split(text, "\n")
			kept := lines[:0]
			for _, line := range lines {
				if strings.Contains(line, "--enable-unsafe-swiftshader") && strings.Contains(line, "chromeArguments.push") {
					continue
				}
				kept = append(kept, line)
			}
			fs.Set(rel, strings.Join(kept, "\n"))
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCredentials",
		Target: "packages/playwright-core/src/server/credentials.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/credentials.ts"
			// Upstream removed credentials.ts in v1.60 (WebAuthn install moved
			// into the driver bundle); the TS patch targets the older layout.
			// Skip when absent so ApplyAll stays green on the pinned version.
			if !fs.Has(rel) {
				return nil
			}
			text := fs.MustGet(rel)
			old := "`(() => {\n      const module = {};\n      ${rawWebAuthnSource.source}\n      module.exports.inject()(globalThis);\n    })();`"
			if !strings.Contains(text, old) {
				// Anchor on the install script declaration more loosely.
				var err error
				text, err = regexpReplace(text,
					"const script = `\\(\\(\\) => \\{[\\s\\S]*?module\\.exports\\.inject\\(\\)\\(globalThis\\);[\\s\\S]*?\\}\\)\\(\\);`",
					"`(() => {\n      const module = {};\n      ${rawWebAuthnSource.source}\n      const installWebAuthn = () => {\n        if (!globalThis.__pwWebAuthnBinding) {\n          setTimeout(installWebAuthn, 0);\n          return;\n        }\n        module.exports.inject()(globalThis);\n      };\n      installWebAuthn();\n    })();`")
				if err != nil {
					return fmt.Errorf("credentials install script anchor: %w", err)
				}
				fs.Set(rel, text)
				return nil
			}
			text = strings.Replace(text, old,
				"`(() => {\n      const module = {};\n      ${rawWebAuthnSource.source}\n      const installWebAuthn = () => {\n        if (!globalThis.__pwWebAuthnBinding) {\n          setTimeout(installWebAuthn, 0);\n          return;\n        }\n        module.exports.inject()(globalThis);\n      };\n      installWebAuthn();\n    })();`", 1)
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchLaunchApp",
		Target: "packages/playwright-core/src/server/launchApp.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/launchApp.ts"
			text := fs.MustGet(rel)
			old := "(window as any)._saveSerializedSettings(JSON.stringify({ ...localStorage }));"
			mustContain(text, old, "syncLocalStorageWithSettings")
			text = strings.Replace(text, old,
				"if (typeof (window as any)._saveSerializedSettings === 'function')\n\t\t\t\t(window as any)._saveSerializedSettings(JSON.stringify({ ...localStorage }));", 1)
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchBuild",
		Target: "utils/build/build.js",
		Apply: func(fs *FileSet) error {
			const rel = "utils/build/build.js"
			text := fs.MustGet(rel)
			// The TS patch replaces the idx initializer (a .search() call in
			// older upstream, .indexOf() in v1.60) with a runtime-require regex.
			oldSearch := "lines[i].search(/node_modules\\//)"
			oldIndexOf := "lines[i].indexOf('node_modules/')"
			replacement := "lines[i].search(/(?:require|import)\\s*\\(\\s*['\"][^'\"]*node_modules\\//)"
			switch {
			case strings.Contains(text, oldSearch):
				text = strings.Replace(text, oldSearch, replacement, 1)
			case strings.Contains(text, oldIndexOf):
				text = strings.Replace(text, oldIndexOf, replacement, 1)
			default:
				return fmt.Errorf("node_modules marker check not found")
			}
			text = strings.ReplaceAll(text,
				"coreBundle.js contains 'node_modules/' references",
				"coreBundle.js contains runtime node_modules paths")
			text = strings.ReplaceAll(text,
				"coreBundle.js: no node_modules/ references",
				"coreBundle.js: no runtime node_modules paths")
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchRecorder",
		Target: "packages/recorder/src/recorder.tsx",
		Apply: func(fs *FileSet) error {
			const rel = "packages/recorder/src/recorder.tsx"
			text := fs.MustGet(rel)
			mustContain(text, "React.useEffect", "Recorder useEffect")
			// Wrap the setAutoExpect effect body in try/catch, mirroring the
			// TS patch (window.dispatch may throw when no dispatcher exists).
			old := "backend.setAutoExpect({ autoExpect });"
			if !strings.Contains(text, old) {
				return fmt.Errorf("recorder setAutoExpect effect not found")
			}
			text = strings.ReplaceAll(text, old,
				"try { window.dispatch({ event: 'setAutoExpect', params: { autoExpect } }); } catch {}")
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCRDevTools",
		Target: "packages/playwright-core/src/server/chromium/crDevTools.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crDevTools.ts"
			text := fs.MustGet(rel)
			mustContain(text, "send('Runtime.enable')", "Runtime.enable in install")
			// Remove Runtime.enable entries from Promise.all arrays in install().
			// Either `this._client.send(...)` or `session.send(...)` forms occur.
			removed := false
			for _, pattern := range []string{
				`\s*this\._client\.send\('Runtime\.enable'\)[^,\n]*,?`,
				`\s*session\.send\('Runtime\.enable'\)[^,\n]*,?`,
			} {
				if updated, err := regexpReplace(text, pattern, ""); err == nil {
					text = updated
					removed = true
				}
			}
			if !removed {
				return fmt.Errorf("Runtime.enable call not removed")
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchCRCoverage",
		Target: "packages/playwright-core/src/server/chromium/crCoverage.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crCoverage.ts"
			text := fs.MustGet(rel)
			anchor := "eventsHelper.addEventListener(this._client, 'Runtime.executionContextsCleared', this._onExecutionContextsCleared.bind(this)),"
			if strings.Count(text, anchor) == 0 {
				return fmt.Errorf("coverage anchor not found")
			}
			text = strings.ReplaceAll(text, anchor,
				anchor+"\n\t\t\teventsHelper.addEventListener(this._client, 'Page.frameNavigated', this._onFrameNavigated.bind(this)),")
			for _, cls := range []string{"class JSCoverage", "class CSSCoverage"} {
				mustContain(text, cls, "coverage class")
			}
			// Append _onFrameNavigated to both coverage classes.
			method := "\n\tasync _onFrameNavigated(event: Protocol.Page.frameNavigatedPayload) {\n\t\tif (event.frame.parentId) return;\n\t\tthis._onExecutionContextsCleared();\n\t}\n"
			for _, cls := range []string{"class JSCoverage", "class CSSCoverage"} {
				var err error
				text, err = appendToClassEnd(text, cls, method)
				if err != nil {
					return fmt.Errorf("%s: %w", cls, err)
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchBrowserContextDispatcher",
		Target: "packages/playwright-core/src/server/dispatchers/browserContextDispatcher.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/dispatchers/browserContextDispatcher.ts"
			text := fs.MustGet(rel)
			mustContain(text, "this._dialogHandler =", "dialog handler assignment")
			var err error
			text, err = regexpReplace(text,
				`this\._dialogHandler\s*=[\s\S]*?;`,
				"this._dialogHandler = dialog => {\n\t\t\tthis._dispatchEvent('dialog', { dialog: new DialogDispatcher(this, dialog) });\n\t\t\treturn true;\n\t\t};")
			if err != nil {
				return err
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchNetwork",
		Target: "packages/playwright-core/src/server/network.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/network.ts"
			text := fs.MustGet(rel)
			mustContain(text, "_rawRequestHeadersPromise", "Request raw headers")
			var err error
			text, err = replaceMethodBody(text, "setRawRequestHeaders",
				MustBody("patchNetwork_setBodyText_01"))
			if err != nil {
				return err
			}
			// Newer upstream renamed internalRawRequestHeaders -> _rawRequestHeaders().
			if strings.Contains(text, "internalRawRequestHeaders") {
				text, err = replaceMethodBody(text, "internalRawRequestHeaders",
					MustBody("patchNetwork_setBodyText_02"))
				if err != nil {
					return err
				}
			} else {
				mustContain(text, "_rawRequestHeaders(", "raw headers accessor")
				text, err = replaceMethodBody(text, "_rawRequestHeaders(",
					"\n\t\treturn this._overrides?.headers || this._rawRequestHeaders || this._rawRequestHeadersPromise;\n\t")
				if err != nil {
					return fmt.Errorf("_rawRequestHeaders: %w", err)
				}
			}
			if !strings.Contains(text, "private _rawRequestHeaders:") && !strings.Contains(text, "_rawRequestHeaders: HeadersArray") {
				text, err = regexpReplace(text,
					`(private _rawRequestHeadersPromise[^;]*;)`,
					"$1\n  private _rawRequestHeaders: HeadersArray | undefined;")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchNetworkDispatchers",
		Target: "packages/playwright-core/src/server/dispatchers/networkDispatchers.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/dispatchers/networkDispatchers.ts"
			text := fs.MustGet(rel)
			mustContain(text, "this._object.continue", "RouteDispatcher.continue")
			if !strings.Contains(text, "patchrightInitScript") {
				var err error
				text, err = regexpReplace(text,
					`(this\._object\.continue\(\{[^}]*?)(\}\))`,
					"$1, patchrightInitScript: (params as any).patchrightInitScript$2")
				if err != nil {
					return err
				}
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchJSHandleDispatcher",
		Target: "packages/playwright-core/src/server/dispatchers/jsHandleDispatcher.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/dispatchers/jsHandleDispatcher.ts"
			text := fs.MustGet(rel)
			mustContain(text, "this._object.evaluateExpression", "JSHandle dispatcher eval")
			// Add params.isolatedContext as the last argument to both dispatcher
			// calls (evaluateExpression and evaluateExpressionHandle).
			re := regexp.MustCompile(`this\._object\.evaluateExpression(Handle)?\(([^;]*?)\)`)
			matches := re.FindAllStringSubmatch(text, -1)
			if len(matches) != 2 {
				return fmt.Errorf("expected 2 JSHandle dispatcher eval calls, found %d", len(matches))
			}
			for _, m := range matches {
				suffix := ""
				if len(m) > 2 && m[1] == "Handle" {
					suffix = "Handle"
				}
				args := m[len(m)-1]
				text = strings.Replace(text, m[0],
					"this._object.evaluateExpression"+suffix+"("+args+", params.isolatedContext)", 1)
			}
			fs.Set(rel, text)
			return nil
		},
	})

	register(PatchFunc{
		Name:   "patchPageDispatcher",
		Target: "packages/playwright-core/src/server/dispatchers/pageDispatcher.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/dispatchers/pageDispatcher.ts"
			text := fs.MustGet(rel)
			mustContain(text, "this._object.evaluateExpression", "Worker dispatcher eval")
			var err error
			text, err = regexpReplace(text,
				`this\._object\.evaluateExpression\(([^)]*)\)`,
				"this._object.evaluateExpression($1, params.isolatedContext)")
			if err != nil {
				return err
			}
			fs.Set(rel, text)
			return nil
		},
	})
}
