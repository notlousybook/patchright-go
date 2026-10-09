package stealth

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

// CallbackBridgeNameRe mirrors the /^f[0-9a-f]{32}$/ gate in
// utilityScriptSerializersPatch.ts and crExecutionContextPatch.ts: exposed
// function bridges are named f + 32 lowercase hex chars so they carry no
// __pw_/playwright fingerprint.
var CallbackBridgeNameRe = regexp.MustCompile(`^f[0-9a-f]{32}$`)

// NewCallbackBridgeName returns a fresh random bridge name (f + 16 random bytes
// as hex). Mirrors `'f' + createGuid()` in jsHandlePatch.ts / clientHelperPatch.ts.
func NewCallbackBridgeName() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "f" + hex.EncodeToString(buf[:]), nil
}

// IsCallbackBridgeName reports whether name matches the bridge gate.
func IsCallbackBridgeName(name string) bool {
	return CallbackBridgeNameRe.MatchString(name)
}

// IsCallbackBridgeDescription mirrors the findFunctions candidate filter in
// crExecutionContextPatch.ts: only `function callbackBridge(` remote objects
// qualify as exposed-function bridges.
func IsCallbackBridgeDescription(description string) bool {
	if len(description) < len("function callbackBridge(") {
		return false
	}
	return description[:len("function callbackBridge(")] == "function callbackBridge("
}
