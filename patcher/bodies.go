package patcher

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed bodies/*.txt bodies/manifest.json
var bodiesFS embed.FS

// BodyEntry describes one extracted TS template-literal replacement body.
type BodyEntry struct {
	SourceFile     string   `json:"sourceFile"`
	PatchFunc      string   `json:"patchFunc"`
	CallKind       string   `json:"callKind"`
	BodyFile       string   `json:"bodyFile"`
	Chars          int      `json:"chars"`
	ContextSymbols []string `json:"contextSymbols"`
}

var (
	bodiesOnce sync.Once
	bodiesByID map[string]string
	bodiesErr  error
	manifest   []BodyEntry
)

func loadBodies() error {
	bodiesOnce.Do(func() {
		raw, err := bodiesFS.ReadFile("bodies/manifest.json")
		if err != nil {
			bodiesErr = err
			return
		}
		if err := json.Unmarshal(raw, &manifest); err != nil {
			bodiesErr = err
			return
		}
		bodiesByID = make(map[string]string, len(manifest))
		for _, e := range manifest {
			data, err := bodiesFS.ReadFile("bodies/" + e.BodyFile)
			if err != nil {
				bodiesErr = fmt.Errorf("read body %s: %w", e.BodyFile, err)
				return
			}
			bodiesByID[strings.TrimSuffix(e.BodyFile, ".txt")] = string(data)
		}
	})
	return bodiesErr
}

// BodiesFor returns all extracted bodies for a patch function, in manifest order.
func BodiesFor(patchFunc string) ([]BodyEntry, error) {
	if err := loadBodies(); err != nil {
		return nil, err
	}
	var out []BodyEntry
	for _, e := range manifest {
		if e.PatchFunc == patchFunc {
			out = append(out, e)
		}
	}
	return out, nil
}

// MustBody returns the embedded body text for a manifest body ID
// (body file name without .txt), panicking on unknown IDs so patch wiring
// mistakes surface loudly at startup.
func MustBody(id string) string {
	if err := loadBodies(); err != nil {
		panic(err)
	}
	body, ok := bodiesByID[id]
	if !ok {
		panic(fmt.Sprintf("patcher: unknown body %q", id))
	}
	return body
}

// Manifest returns the full body manifest.
func Manifest() ([]BodyEntry, error) {
	if err := loadBodies(); err != nil {
		return nil, err
	}
	return manifest, nil
}
