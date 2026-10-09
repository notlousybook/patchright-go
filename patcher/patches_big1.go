package patcher

import (
	"fmt"
	"strings"
)

// This file ports patchCRNetworkManager and patchCRPage: the always-on
// init-script injection via Fetch interception (with CSP fixing,
// head injection, Set-Cookie preservation) and the Runtime.enable-avoidance
// frame-session rewiring.

func init() {
	register(PatchFunc{
		Name:   "patchCRNetworkManager",
		Target: "packages/playwright-core/src/server/chromium/crNetworkManager.ts",
		Apply: func(fs *FileSet) error {
			const rel = "packages/playwright-core/src/server/chromium/crNetworkManager.ts"
			text := fs.MustGet(rel)
			var err error

			// crypto import for randomBytes script ids.
			if !strings.Contains(text, `from "crypto"`) && !strings.Contains(text, `from 'crypto'`) {
				mustContain(text, "import", "import block")
				text, err = regexpReplace(text, `(import[^\n]*\n)`, "$1import crypto from \"crypto\";\n")
				if err != nil {
					return err
				}
			}
			// _alreadyTrackedNetworkIds property.
			if !strings.Contains(text, "_alreadyTrackedNetworkIds") {
				mustContain(text, "class CRNetworkManager", "CRNetworkManager class")
				text, err = regexpReplace(text,
					`(class CRNetworkManager[^{]*\{)`,
					"$1\n\tprivate _alreadyTrackedNetworkIds = new Set<string>();")
				if err != nil {
					return err
				}
			}
			// removeSession: clear tracking set when no sessions remain.
			mustContain(text, "this._sessions.delete(session)", "removeSession")
			if !strings.Contains(text, "this._alreadyTrackedNetworkIds.clear()") {
				text = strings.Replace(text,
					"this._sessions.delete(session)",
					"this._sessions.delete(session);\n\t\tif (!this._sessions.size) this._alreadyTrackedNetworkIds.clear();", 1)
			}
			// _onRequest: RouteImpl ctor gains (page, networkId, sessionManager).
			oldRoute := "new RouteImpl(requestPausedSessionInfo!.session, requestPausedEvent.requestId"
			mustContain(text, oldRoute, "RouteImpl construction")
			text = strings.Replace(text, oldRoute,
				strings.TrimSpace(MustBody("patchCRNetworkManager_replaceWithText_01")), 1)
			// _updateProtocolRequestInterceptionForSession: cache policy.
			oldCache := "const cachePromise = info.session.send('Network.setCacheDisabled', { cacheDisabled: enabled });"
			mustContain(text, oldCache, "cache policy")
			text = strings.Replace(text, oldCache,
				strings.TrimSpace(MustBody("patchCRNetworkManager_replaceWithText_02")), 1)
			// setRequestInterception: sync cache state.
			mustContain(text, "setRequestInterception", "setRequestInterception")
			if !strings.Contains(text, "Network.setCacheDisabled', { cacheDisabled: this._page.needsRequestInterception() }") {
				// Append the statement at the end of setRequestInterception via method replace.
				sigIdx := strings.Index(text, "setRequestInterception(")
				if sigIdx < 0 {
					return fmt.Errorf("setRequestInterception not found")
				}
				pOpen := strings.Index(text[sigIdx:], "(") + sigIdx
				pEnd, err := parenEnd(text, pOpen)
				if err != nil {
					return fmt.Errorf("setRequestInterception params: %w", err)
				}
				open := strings.Index(text[pEnd:], "{") + pEnd
				end, err := braceBlockEnd(text, open)
				if err != nil {
					return err
				}
				text = text[:end-1] + MustBody("patchCRNetworkManager_addStatements_01") + text[end-1:]
			}
			// _onRequest head: tracked-network dedup.
			if !strings.Contains(text, "this._alreadyTrackedNetworkIds.has(requestWillBeSentEvent.requestId)") {
				sigIdx := strings.Index(text, "async _onRequest(")
				if sigIdx < 0 {
					sigIdx = strings.Index(text, "_onRequest(")
				}
				if sigIdx < 0 {
					return fmt.Errorf("_onRequest not found")
				}
				open := strings.Index(text[sigIdx:], "{") + sigIdx
				text = text[:open+1] + "\n" + MustBody("patchCRNetworkManager_insertStatements_01") + text[open+1:]
			}
			// OPTIONS preflight bypass after the isInterceptedOptionsPreflight decl.
			if !strings.Contains(text, "isInterceptedOptionsPreflight && !(this._page") {
				mustContain(text, "const isInterceptedOptionsPreflight", "preflight decl")
				text = strings.Replace(text, "const isInterceptedOptionsPreflight",
					strings.TrimSpace(MustBody("patchCRNetworkManager_insertStatements_02"))+"\n\t\tconst isInterceptedOptionsPreflight", 1)
			}
			// Detached-page delegate guard.
			oldGuard := "if (!frame && this._page && requestWillBeSentEvent.frameId === (this._page?.delegate)._targetId)"
			if strings.Contains(text, oldGuard) {
				text = strings.Replace(text, oldGuard,
					strings.TrimSpace(MustBody("patchCRNetworkManager_replaceWithText_03")), 1)
			}
			// Provisional-headers condition narrowing (best effort).
			if strings.Contains(text, "request.request.setRawRequestHeaders") {
				if updated, err := regexpReplace(text,
					`if\s*\(route\)(\s*\{[^}]*request\.request\.setRawRequestHeaders)`,
					"if (route && (this._page?.needsRequestInterception() || !!this._serviceWorker))$1"); err == nil {
					text = updated
				}
			}
			// _onRequestPaused head: replay intercepted events.
			if !strings.Contains(text, "_originalRequestRoute?._networkRequestIntercepted(event)") {
				sigIdx := strings.Index(text, "_onRequestPaused(")
				if sigIdx < 0 {
					return fmt.Errorf("_onRequestPaused not found")
				}
				open := strings.Index(text[sigIdx:], "{") + sigIdx
				text = text[:open+1] + "\n" + MustBody("patchCRNetworkManager_insertStatements_03") + text[open+1:]
			}
			// _onRequestServedFromCache tail: pair cached requests.
			if !strings.Contains(text, "this._requestIdToRequestWillBeSentEvent.delete(event.requestId)") {
				methIdx := strings.Index(text, "_onRequestServedFromCache(")
				if methIdx < 0 {
					return fmt.Errorf("_onRequestServedFromCache not found")
				}
				open := strings.Index(text[methIdx:], "{") + methIdx
				end, err := braceBlockEnd(text, open)
				if err != nil {
					return err
				}
				text = text[:end-1] + MustBody("patchCRNetworkManager_addStatements_02") + text[end-1:]
			}
			// RouteImpl fields: _fulfilledSetCookieHeaders + _fulfilledResponse.
			if !strings.Contains(text, "_fulfilledSetCookieHeaders") {
				mustContain(text, "_fulfilled", "RouteImpl._fulfilled")
				text, err = regexpReplace(text,
					`(_fulfilled[^;]*;)`,
					"$1\n\tprivate _fulfilledSetCookieHeaders: types.HeadersArray = [];\n\tprivate _fulfilledResponse: Pick<Protocol.Network.Response, \"status\" | \"statusText\" | \"headers\"> | undefined;")
				if err != nil {
					return err
				}
			}
			// RouteImpl ctor params (page, networkId, sessionManager).
			if !strings.Contains(text, "sessionManager: CRNetworkManager") {
				mustContain(text, "constructor(", "RouteImpl ctor")
				text, err = regexpReplace(text,
					`(constructor\(\s*session:[^)]*interceptionId:[^)]*)(\))`,
					"$1, page: Page | null, networkId: string, sessionManager: CRNetworkManager$2")
				if err != nil {
					return err
				}
				text = strings.Replace(text,
					"this._page = page;\n\t\tthis._networkId = networkId;\n\t\tthis._sessionManager = sessionManager;",
					"this._page = page;\n\t\tthis._networkId = networkId;\n\t\tthis._sessionManager = sessionManager;", 1)
				// Ensure assignments exist at ctor end.
				if !strings.Contains(text, "this._sessionManager = sessionManager;") {
					cIdx := strings.Index(text, "class RouteImpl")
					oBrace := strings.Index(text[cIdx:], "{") + cIdx
					_ = oBrace
					// Append assignments before the ctor's closing brace: anchor on ctor signature.
					ctorIdx := strings.Index(text[cIdx:], "constructor(")
					ctorOpen := strings.Index(text[cIdx+ctorIdx:], "{") + cIdx + ctorIdx
					ctorEnd, err := braceBlockEnd(text, ctorOpen)
					if err != nil {
						return err
					}
					assign := "\n\t\tthis._page = page;\n\t\tthis._networkId = networkId;\n\t\tthis._sessionManager = sessionManager;\n\t"
					text = text[:ctorEnd-1] + assign + text[ctorEnd-1:]
				}
			}
			// RouteImpl._fixCSP + _injectIntoHead: new methods.
			if !strings.Contains(text, "async _fixCSP(") && !strings.Contains(text, "_fixCSP(csp") {
				text, err = appendToClassEnd(text, "class RouteImpl",
					"\t_fixCSP(csp: string | null, scriptNonce: string | null) {"+
						MustBody("patchCRNetworkManager_setBodyText_01")+"\n\t}"+
						"\n\t_injectIntoHead(body: string, injectionHTML: string) {"+
						MustBody("patchCRNetworkManager_setBodyText_02")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// RouteImpl.fulfill / continue / _networkRequestIntercepted bodies.
			text, err = replaceMethodBody(text, "fulfill(",
				MustBody("patchCRNetworkManager_setBodyText_03"))
			if err != nil {
				return fmt.Errorf("fulfill: %w", err)
			}
			text, err = replaceMethodBody(text, "continue(",
				MustBody("patchCRNetworkManager_setBodyText_04"))
			if err != nil {
				// continue( may collide with the keyword; anchor on the method decl.
				mIdx := strings.Index(text, "async continue(")
				if mIdx < 0 {
					mIdx = strings.Index(text, "continue(")
				}
				if mIdx < 0 {
					return fmt.Errorf("continue: %w", err)
				}
				open := strings.Index(text[mIdx:], "{") + mIdx
				end, err2 := braceBlockEnd(text, open)
				if err2 != nil {
					return err2
				}
				text = text[:open+1] + MustBody("patchCRNetworkManager_setBodyText_04") + text[end-1:]
			}
			if !strings.Contains(text, "async _networkRequestIntercepted(") {
				text, err = appendToClassEnd(text, "class RouteImpl",
					"\tasync _networkRequestIntercepted(event: Protocol.Fetch.requestPausedPayload) {"+
						MustBody("patchCRNetworkManager_setBodyText_05")+"\n\t}")
				if err != nil {
					return err
				}
			}
			// _createResponse: Set-Cookie preservation.
			mustContain(text, "new network.Response(", "_createResponse")
			if !strings.Contains(text, "_originalRequestRoute?._fulfilledSetCookieHeaders") {
				oldNew := "const response = new network.Response("
				mustContain(text, oldNew, "_createResponse decl")
				text = strings.Replace(text, oldNew,
					strings.TrimSpace(MustBody("patchCRNetworkManager_insertStatements_04"))+"\n\t\t"+oldNew, 1)
				mustContain(text, "const response = new network.Response(", "updated decl")
				text = strings.Replace(text, "const response = new network.Response(",
					"const response = new network.Response(", 1)
				// responseHeaders arg swap: 4th ctor arg -> responseHeaders, then setRaw.
				text, err = regexpReplace(text,
					`(new network\.Response\([^,]+,[^,]+,[^,]+,)([^,)]+)`,
					"$1 responseHeaders")
				if err != nil {
					return fmt.Errorf("responseHeaders swap: %w", err)
				}
				text = strings.Replace(text, "const response = new network.Response(",
					strings.TrimSpace(MustBody("patchCRNetworkManager_insertStatements_06"))+"\n\t\tconst response = new network.Response(", 1)
			}
			// _onLoadingFinished: fulfilled service-worker response replay.
			if !strings.Contains(text, "request._originalRequestRoute?._fulfilledResponse") {
				mustContain(text, "_onLoadingFinished(", "_onLoadingFinished")
				lIdx := strings.Index(text, "_onLoadingFinished(")
				lOpen := strings.Index(text[lIdx:], "{") + lIdx
				// Insert after `let response` declaration.
				declIdx := strings.Index(text[lOpen:], "response")
				_ = declIdx
				semi := strings.Index(text[lOpen:], ";")
				if semi < 0 {
					return fmt.Errorf("_onLoadingFinished anchor")
				}
				pos := lOpen + semi + 1
				text = text[:pos] + "\n" + MustBody("patchCRNetworkManager_insertStatements_05") + text[pos:]
			}
			fs.Set(rel, text)
			return nil
		},
	})
}
