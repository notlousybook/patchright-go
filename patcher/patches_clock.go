package patcher

import (
	"fmt"
	"strings"
)

// ApplyClock ports clockPatch.ts (server/clock.ts): it replaces the
// `if (this._initScripts.length)` guard in Clock._installIfNeeded with a
// version that evals init-script sources inline (no __pwClock globals leak),
// and prepends frame-warming to Clock._evaluateInFrames.
func ApplyClock(text string, fs *FileSet, rel string) error {
	mustContain(text, "this._initScripts.length", "clock install guard")
	// Replace the guard block: find `if (this._initScripts.length) {` and its
	// matching close, then splice the patchright replacement.
	guardIdx := strings.Index(text, "if (this._initScripts.length)")
	if guardIdx < 0 {
		return fmt.Errorf("clock guard not found")
	}
	open := strings.Index(text[guardIdx:], "{")
	if open < 0 {
		return fmt.Errorf("clock guard body not found")
	}
	open += guardIdx
	end, err := braceBlockEnd(text, open)
	if err != nil {
		return err
	}
	replacement := "{\n" + MustBody("patchClock_replaceWithText_01") + "\n\t\t}"
	text = text[:open] + replacement + text[end:]

	// _evaluateInFrames: prepend frame warming.
	mustContain(text, "_evaluateInFrames", "clock _evaluateInFrames")
	warm := MustBody("patchClock_insertStatements_01")
	sigIdx := strings.Index(text, "_evaluateInFrames(")
	if sigIdx < 0 {
		return fmt.Errorf("clock _evaluateInFrames not found")
	}
	bodyOpen := strings.Index(text[sigIdx:], "{")
	if bodyOpen < 0 {
		return fmt.Errorf("clock _evaluateInFrames body not found")
	}
	bodyOpen += sigIdx
	text = text[:bodyOpen+1] + "\n" + warm + text[bodyOpen+1:]

	fs.Set(rel, text)
	return nil
}
