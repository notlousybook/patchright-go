package patcher

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// This file ports the protocol/spec/*.yml mutations at the tail of
// patchright_driver_patch.ts plus the package-rename half of
// patchright_rebranding.ts (renameImportsAndExportsInDirectory).

// ProtocolMutations mirrors the five mutateYaml calls.
func ProtocolMutations() map[string]func(map[string]any) {
	setParam := func(cmdPath string, param string) func(map[string]any) {
		return func(doc map[string]any) {
			node := doc
			for _, key := range strings.Split(cmdPath, ".") {
				next, ok := node[key].(map[string]any)
				if !ok {
					next = map[string]any{}
					node[key] = next
				}
				node = next
			}
			params, ok := node["parameters"].(map[string]any)
			if !ok {
				params = map[string]any{}
				node["parameters"] = params
			}
			params[param] = "boolean?"
		}
	}
	return map[string]func(map[string]any){
		"packages/protocol/spec/frame.yml": func(doc map[string]any) {
			for _, cmd := range []string{"evaluateExpression", "evaluateExpressionHandle", "evalOnSelectorAll"} {
				setParam("Frame.commands."+cmd, "isolatedContext")(doc)
			}
		},
		"packages/protocol/spec/handles.yml": func(doc map[string]any) {
			for _, cmd := range []string{"evaluateExpression", "evaluateExpressionHandle"} {
				setParam("JSHandle.commands."+cmd, "isolatedContext")(doc)
			}
		},
		"packages/protocol/spec/worker.yml": func(doc map[string]any) {
			for _, cmd := range []string{"evaluateExpression", "evaluateExpressionHandle"} {
				setParam("Worker.commands."+cmd, "isolatedContext")(doc)
			}
		},
		"packages/protocol/spec/mixins.yml": func(doc map[string]any) {
			setParam("ContextOptions.properties", "focusControl")(doc)
		},
		"packages/protocol/spec/network.yml": func(doc map[string]any) {
			setParam("Route.commands.continue", "patchrightInitScript")(doc)
		},
	}
}

// MutateProtocolYAML applies one mutation func to a YAML document.
func MutateProtocolYAML(text string, mutate func(map[string]any)) (string, error) {
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	mutate(doc)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func init() {
	register(PatchFunc{
		Name: "patchProtocol",
		Apply: func(fs *FileSet) error {
			for rel, mutate := range ProtocolMutations() {
				if !fs.Has(rel) {
					continue // protocol specs may be absent in sparse checkouts
				}
				text := fs.MustGet(rel)
				updated, err := MutateProtocolYAML(text, mutate)
				if err != nil {
					return fmt.Errorf("%s: %w", rel, err)
				}
				fs.Set(rel, updated)
			}
			return nil
		},
	})

	register(PatchFunc{
		Name: "patchRebrandPackages",
		Apply: func(fs *FileSet) error {
			// Ports renameImportsAndExportsInDirectory + package renames for the
			// two npm packages. Only rewrites module specifiers; directory and
			// package.json renames happen at publish time (see cmd/patchright).
			for rel := range fs.files {
				if !strings.HasPrefix(rel, "packages/patchright") && !strings.HasPrefix(rel, "packages/playwright") {
					continue
				}
				if !(strings.HasSuffix(rel, ".ts") || strings.HasSuffix(rel, ".js") || strings.HasSuffix(rel, ".mjs")) {
					continue
				}
				text := fs.files[rel]
				updated := strings.ReplaceAll(text, "playwright-core", "patchright-core")
				// require()/import() catch-alls mentioning bare "playwright".
				updated = strings.ReplaceAll(updated, `"playwright"`, `"patchright"`)
				updated = strings.ReplaceAll(updated, `'playwright'`, `'patchright'`)
				if updated != text {
					fs.Set(rel, updated)
				}
			}
			return nil
		},
	})
}
