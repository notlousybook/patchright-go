package client

import (
	"fmt"
)

// This file holds the pure parts of isolated evaluation: function-declaration
// building, CDP CallArgument marshalling, and RemoteObject decoding. They are
// deliberately free of playwright-go types so they can be unit-tested against
// raw CDP payloads — the shape where encoding bugs actually hide.

// buildFunctionDeclaration turns a patchright-style pageFunction + arg into a
// JS function declaration plus its marshalled arguments.
//
// Accepted forms:
//   - func(...) any / func(any) any: the function body is derived by rendering
//     the closure as JS is not possible in general, so these are NOT supported.
//     Use the source-string forms below.
//   - string containing '=>' or 'function': treated as a JS expression and
//     wrapped so the single argument is passed to it.
//   - string otherwise: treated as a JS expression body, evaluated with the
//     argument in scope as `arg`.
//
// This mirrors what patchright clients do: the common case is a JS source
// string, optionally a function expression.
func buildFunctionDeclaration(pageFunction any, arg any) (string, []any, error) {
	switch fn := pageFunction.(type) {
	case string:
		src := trimSpace(fn)
		if src == "" {
			return "", nil, fmt.Errorf("pageFunction is empty")
		}
		// Arrow or function expression: call it directly with the argument.
		if isArrow(src) || hasPrefix(src, "function") || hasPrefix(src, "async function") {
			return "(" + src + ")", []any{arg}, nil
		}
		// Bare expression: evaluate it.
		return "(() => (" + src + "))()", nil, nil
	case func() any:
		return "", nil, fmt.Errorf("func() any is not supported remotely; pass a JS source string")
	default:
		return "", nil, fmt.Errorf("unsupported pageFunction type %T", pageFunction)
	}
}

// toCallArguments marshals Go values into CDP CallArgument shape.
func toCallArguments(args []any) []map[string]any {
	out := make([]map[string]any, 0, len(args))
	for _, a := range args {
		out = append(out, map[string]any{"value": a})
	}
	return out
}

// DecodeRemoteObject turns a CDP RemoteObject into a Go value, applying the
// same precedence patchright's clients use:
// unserializableValue (Infinity/-0/NaN) > explicit null/undefined > value >
// objectId (a handle we cannot read without a driver-side JSHandle).
func DecodeRemoteObject(obj any) any {
	m, ok := obj.(map[string]any)
	if !ok {
		return obj
	}
	// Unserializable primitives win: they carry no usable value field.
	if uv, ok := stringField(m, "unserializableValue"); ok {
		switch uv {
		case "Infinity", "-Infinity":
			return nil // CDP spells these as doubles elsewhere; keep it simple
		case "-0":
			return 0
		case "NaN":
			return nil
		}
		return uv
	}
	if subtype, ok := stringField(m, "subtype"); ok {
		switch subtype {
		case "null":
			return nil
		case "error":
			// Exception object surfaces via exceptionDetails instead.
			return nil
		}
	}
	if t, ok := stringField(m, "type"); ok && t == "undefined" {
		return nil
	}
	if v, ok := m["value"]; ok {
		return normalizeCDPValue(v)
	}
	// No readable value (an object we did not ask to serialize).
	if _, ok := stringField(m, "objectId"); ok {
		return fmt.Sprintf("<js-handle:%s>", mustString(m, "objectId"))
	}
	return nil
}

// normalizeCDPValue fixes up CDP's JSON quirks: undefined inside arrays comes
// back as {"type":"undefined"} objects, which would otherwise surface as maps
// to Go callers.
func normalizeCDPValue(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		if t, ok := stringField(tv, "type"); ok && t == "undefined" {
			return nil
		}
		out := make(map[string]any, len(tv))
		for k, item := range tv {
			out[k] = normalizeCDPValue(item)
		}
		return out
	case []any:
		out := make([]any, len(tv))
		for i, item := range tv {
			out[i] = normalizeCDPValue(item)
		}
		return out
	default:
		return v
	}
}

func exceptionDescription(exc any) string {
	m, ok := exc.(map[string]any)
	if !ok {
		return fmt.Sprintf("%v", exc)
	}
	if d, ok := stringField(m, "exception"); ok {
		return d
	}
	if desc, ok := stringField(m, "text"); ok {
		return desc
	}
	return fmt.Sprintf("%v", exc)
}
