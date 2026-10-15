package client

import (
	"fmt"
	"sync"

	pw "github.com/playwright-community/playwright-go"
)

// This file closes the one real parity gap between patchright-go and the other
// patchright clients: isolated-world evaluation.
//
// Upstream patchright clients default `evaluate` to running in a dedicated
// execution context (isolatedContext: true) rather than the page's main world.
// The plain playwright-go binding cannot express that flag — its Evaluate has
// no such parameter — so without this file `page.Evaluate` ran in the main
// world, which is precisely what the isolated-world design exists to avoid.
//
// We reach it through the CDP seam playwright-go does expose: open a CDP
// session on the page, create an isolated world, and call the function in that
// world with Runtime.callFunctionOn. No forking of the binding required.

// isolatedWorld is one page's dedicated execution context, created lazily.
type isolatedWorld struct {
	session   pw.CDPSession
	frameID   string
	contextID float64
}

var (
	worldsMu sync.Mutex
	worlds   = map[pw.Page]*isolatedWorld{}
)

// isolatedContextName is the world name patchright-style isolated evaluation
// runs in. Deliberately neutral: nothing to correlate back to automation.
const isolatedContextName = "pw_iso"

// isolatedWorldFor returns (creating if needed) the isolated execution context
// for page. Worlds are cached because Page.createIsolatedWorld is per-frame and
// must not be re-created on every evaluate — that would leak contexts.
func isolatedWorldFor(page pw.Page) (*isolatedWorld, error) {
	worldsMu.Lock()
	defer worldsMu.Unlock()
	if w, ok := worlds[page]; ok {
		return w, nil
	}
	sess, err := page.Context().NewCDPSession(page)
	if err != nil {
		return nil, fmt.Errorf("cdp session: %w", err)
	}
	// The main frame id is what Page.createIsolatedWorld wants.
	tree, err := sess.Send("Page.getFrameTree", nil)
	if err != nil {
		sess.Detach()
		return nil, fmt.Errorf("getFrameTree: %w", err)
	}
	frameID, err := mainFrameID(tree)
	if err != nil {
		sess.Detach()
		return nil, err
	}
	created, err := sess.Send("Page.createIsolatedWorld", map[string]any{
		"frameId":              frameID,
		"worldName":            isolatedContextName,
		"grantUniversalAccess": true,
	})
	if err != nil {
		sess.Detach()
		return nil, fmt.Errorf("createIsolatedWorld: %w", err)
	}
	ctxID, ok := numField(asMap(created), "executionContextId")
	if !ok {
		sess.Detach()
		return nil, fmt.Errorf("createIsolatedWorld returned no executionContextId")
	}
	w := &isolatedWorld{session: sess, frameID: frameID, contextID: ctxID}
	worlds[page] = w
	return w, nil
}

// asMap is the CDP payload shape returned by CDPSession.Send.
func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// evaluateIsolated runs functionDeclaration in page's isolated world and
// decodes the result. awaitPromise mirrors patchright's evaluate semantics.
func evaluateIsolated(page pw.Page, functionDeclaration string, args []any) (any, error) {
	w, err := isolatedWorldFor(page)
	if err != nil {
		return nil, err
	}
	res, err := w.session.Send("Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": functionDeclaration,
		"executionContextId":  w.contextID,
		"arguments":           toCallArguments(args),
		"returnByValue":       true,
		"awaitPromise":        true,
		"userGesture":         true,
	})
	if err != nil {
		// A stale context (page navigated to a new document) is the one
		// recoverable failure: drop the cache and retry once on a fresh world.
		worldsMu.Lock()
		delete(worlds, page)
		worldsMu.Unlock()
		w.session.Detach()
		w2, err2 := isolatedWorldFor(page)
		if err2 != nil {
			return nil, fmt.Errorf("callFunctionOn: %w (recreate failed: %v)", err, err2)
		}
		res, err = w2.session.Send("Runtime.callFunctionOn", map[string]any{
			"functionDeclaration": functionDeclaration,
			"executionContextId":  w2.contextID,
			"arguments":           toCallArguments(args),
			"returnByValue":       true,
			"awaitPromise":        true,
			"userGesture":         true,
		})
		if err != nil {
			return nil, fmt.Errorf("callFunctionOn: %w", err)
		}
	}
	if exception, ok := mapField(asMap(res), "exceptionDetails"); ok {
		return nil, fmt.Errorf("evaluation failed: %v", exceptionDescription(exception))
	}
	result, ok := mapField(asMap(res), "result")
	if !ok {
		return nil, fmt.Errorf("callFunctionOn returned no result")
	}
	return DecodeRemoteObject(result), nil
}

// EvaluateIsolated evaluates expression in page's isolated context, matching
// patchright's default `evaluate(pageFunction, arg)` semantics.
//
// pageFunction may be a func(any) any, a func() any, or a JS source string.
// The function form is wrapped so its single argument is the serialized arg,
// exactly like patchright's evaluate.
func (in *Instance) EvaluateIsolated(page pw.Page, pageFunction any, arg any) (any, error) {
	decl, args, err := buildFunctionDeclaration(pageFunction, arg)
	if err != nil {
		return nil, err
	}
	return evaluateIsolated(page, decl, args)
}

// EvaluateHandleIsolated evaluates in the isolated context and returns the raw
// CDP objectId of the result instead of its value.
//
// KNOWN LIMITATION: playwright-go's JSHandle is driver-backed, so a handle
// cannot be fabricated from a CDP object id. Callers needing a real JSHandle
// must use pw's own EvaluateHandle (main world). Isolated value evaluation —
// the part that matters for detection exposure — is fully supported.
func (in *Instance) EvaluateHandleIsolated(page pw.Page, pageFunction any, arg any) (string, error) {
	decl, args, err := buildFunctionDeclaration(pageFunction, arg)
	if err != nil {
		return "", err
	}
	w, err := isolatedWorldFor(page)
	if err != nil {
		return "", err
	}
	res, err := w.session.Send("Runtime.callFunctionOn", map[string]any{
		"functionDeclaration": decl,
		"executionContextId":  w.contextID,
		"arguments":           toCallArguments(args),
		"returnByValue":       false,
		"awaitPromise":        true,
		"userGesture":         true,
	})
	if err != nil {
		return "", fmt.Errorf("callFunctionOn: %w", err)
	}
	result, ok := mapField(asMap(res), "result")
	if !ok {
		return "", fmt.Errorf("callFunctionOn returned no result")
	}
	if id, ok := stringField(result, "objectId"); ok {
		return id, nil
	}
	// Primitive result: there is no object to hold a handle to.
	return "", nil
}

// releaseIsolatedWorld drops the cached world for page (call after the page is
// closed, otherwise the CDP session and context id leak).
func releaseIsolatedWorld(page pw.Page) {
	worldsMu.Lock()
	w, ok := worlds[page]
	delete(worlds, page)
	worldsMu.Unlock()
	if ok {
		w.session.Detach()
	}
}
