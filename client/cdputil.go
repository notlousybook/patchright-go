package client

import (
	"fmt"
	"strings"
)

// Small shared helpers for CDP payloads and JS source sniffing.

func trimSpace(s string) string { return strings.TrimSpace(s) }

func hasPrefix(s, p string) bool { return strings.HasPrefix(s, p) }

// isArrow reports whether a JS source string is an arrow function expression
// (covers `x => ...`, `(x) => ...`, `async (x) => ...`).
func isArrow(src string) bool {
	depth := 0
	for i := 0; i+1 < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '\'', '"', '`':
			// skip string bodies
			q := src[i]
			i++
			for i < len(src) && src[i] != q {
				if src[i] == '\\' {
					i++
				}
				i++
			}
		case '=':
			if depth == 0 && src[i+1] == '>' {
				return true
			}
		}
	}
	return false
}

// stringField reads a string value from a CDP map field.
func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// mapField reads a nested object from a CDP map field.
func mapField(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	sub, ok := v.(map[string]any)
	return sub, ok
}

// numField reads a number (CDP sends ints as float64 in JSON).
func numField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return f, ok
}

func mustString(m map[string]any, key string) string {
	s, _ := stringField(m, key)
	return s
}

// mainFrameID extracts the main frame id from a Page.getFrameTree response.
func mainFrameID(tree any) (string, error) {
	m, ok := tree.(map[string]any)
	if !ok {
		return "", fmt.Errorf("getFrameTree: unexpected shape %T", tree)
	}
	ft, ok := mapField(m, "frameTree")
	if !ok {
		return "", fmt.Errorf("getFrameTree: no frameTree")
	}
	frame, ok := mapField(ft, "frame")
	if !ok {
		return "", fmt.Errorf("getFrameTree: no frame")
	}
	id, ok := stringField(frame, "id")
	if !ok || id == "" {
		return "", fmt.Errorf("getFrameTree: no frame id")
	}
	return id, nil
}
