package client

import (
	"reflect"
	"strings"
	"testing"
)

// These cover the pure part of isolated evaluation. The browser-bound half
// (world creation, callFunctionOn) is exercised by the e2e suite; here we test
// the shapes where encoding bugs actually hide.

func TestDecodeRemoteObject(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"string", map[string]any{"type": "string", "value": "hi"}, "hi"},
		{"number", map[string]any{"type": "number", "value": float64(42)}, float64(42)},
		{"bool", map[string]any{"type": "boolean", "value": false}, false},
		{"null subtype", map[string]any{"type": "object", "subtype": "null"}, nil},
		{"undefined", map[string]any{"type": "undefined"}, nil},
		{"nan", map[string]any{"unserializableValue": "NaN"}, nil},
		{"infinity", map[string]any{"unserializableValue": "Infinity"}, nil},
		{"object with value", map[string]any{"type": "object", "value": map[string]any{"a": float64(1)}}, map[string]any{"a": float64(1)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeRemoteObject(c.in)
			if !deepEqualish(got, c.want) {
				t.Errorf("got %#v, want %#v", got, c.want)
			}
		})
	}
}

func TestNormalizeCDPValue(t *testing.T) {
	// CDP renders an undefined array element as a nested object; Go callers
	// must see nil, or their `if x == nil` checks silently lie.
	in := []any{
		map[string]any{"type": "number", "value": float64(1)},
		map[string]any{"type": "undefined"},
		map[string]any{"nested": map[string]any{"type": "undefined"}},
	}
	out := normalizeCDPValue(in).([]any)
	if out[1] != nil {
		t.Errorf("array element 1 = %#v, want nil", out[1])
	}
	nested, ok := out[2].(map[string]any)
	if !ok {
		t.Fatalf("element 2 lost its map: %#v", out[2])
	}
	if nested["nested"] != nil {
		t.Errorf("nested undefined = %#v, want nil", nested["nested"])
	}
}

func TestDecodeRemoteObjectHandle(t *testing.T) {
	// A result we did not ask to serialize comes back as an objectId. Telling
	// the caller "<js-handle:...>" is far better than handing them nil, which
	// would read as "evaluated to null".
	got := DecodeRemoteObject(map[string]any{"objectId": "obj-123"})
	s, ok := got.(string)
	if !ok || !strings.Contains(s, "obj-123") {
		t.Errorf("got %#v, want handle placeholder mentioning obj-123", got)
	}
}

func TestToCallArguments(t *testing.T) {
	got := toCallArguments([]any{"a", float64(2), nil})
	if len(got) != 3 {
		t.Fatalf("want 3 args, got %d", len(got))
	}
	if got[0]["value"] != "a" {
		t.Errorf("arg 0 = %#v", got[0])
	}
	if got[2]["value"] != nil {
		t.Errorf("arg 2 must be explicit JSON null, got %#v", got[2])
	}
}

func TestBuildFunctionDeclaration(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"arrow", "(a) => a + 1", "((a) => a + 1)"},
		{"bare arrow", "a => a", "(a => a)"},
		{"async arrow", "async (a) => a", "(async (a) => a)"},
		{"function expr", "function () { return 1; }", "(function () { return 1; })"},
		{"async function", "async function () { return 1; }", "(async function () { return 1; })"},
		{"bare expr", "1 + 1", "(() => (1 + 1))()"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decl, args, err := buildFunctionDeclaration(c.in, nil)
			if err != nil {
				t.Fatal(err)
			}
			if decl != c.want {
				t.Errorf("decl = %q, want %q", decl, c.want)
			}
			if c.name == "bare expr" {
				if len(args) != 0 {
					t.Errorf("bare expression should take no args, got %v", args)
				}
			} else if len(args) != 1 {
				t.Errorf("callable form should pass one arg, got %v", args)
			}
		})
	}
}

func TestBuildFunctionDeclarationRejects(t *testing.T) {
	if _, _, err := buildFunctionDeclaration("", nil); err == nil {
		t.Error("empty source should fail, not evaluate nothing")
	}
	if _, _, err := buildFunctionDeclaration(func() any { return 1 }, nil); err == nil {
		t.Error("a Go closure cannot run remotely; must fail loudly")
	}
}

func TestIsArrow(t *testing.T) {
	if !isArrow("(a) => a") || !isArrow("a => a") || !isArrow("async () => 1") {
		t.Error("arrow forms not detected")
	}
	// The arrow-looking token inside a string must not count.
	if isArrow("'a => b'") {
		t.Error("string contents misread as an arrow")
	}
	if isArrow("document.querySelector('#x')") {
		t.Error("plain statement misread as an arrow")
	}
}

func TestMainFrameID(t *testing.T) {
	tree := map[string]any{"frameTree": map[string]any{"frame": map[string]any{"id": "FRAME1"}}}
	id, err := mainFrameID(tree)
	if err != nil || id != "FRAME1" {
		t.Errorf("got %q %v", id, err)
	}
	for _, bad := range []any{
		nil,
		map[string]any{},
		map[string]any{"frameTree": map[string]any{}},
		map[string]any{"frameTree": map[string]any{"frame": map[string]any{}}},
	} {
		if _, err := mainFrameID(bad); err == nil {
			t.Errorf("bad payload %#v accepted", bad)
		}
	}
}

func deepEqualish(a, b any) bool {
	return reflect.DeepEqual(a, b)
}
