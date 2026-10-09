// Package patcher is the whole patchright driver patch pipeline, in go.
// it takes a stock microsoft/playwright checkout and applies all 47 stealth
// transforms (used to be patchright_driver_patch.ts + driver_patches/ +
// client_patches/ in typescript). replacement JS bodies are embedded in the
// binary, extracted verbatim from the original TS patch sources.
package patcher

// DefaultPlaywrightVersion is the fallback when no version is given and the
// latest release can't be resolved. It's also the version the patches are
// validated against (go test -tags checkout ./patcher/).
const DefaultPlaywrightVersion = "v1.62.0"

// PlaywrightVersion is kept for backwards compat, same as DefaultPlaywrightVersion.
//
// Deprecated: use DefaultPlaywrightVersion, or better yet don't pin anything
// and let the CLI resolve latest.
const PlaywrightVersion = DefaultPlaywrightVersion

// PlaywrightRepo is the upstream driver repository the patcher operates on.
const PlaywrightRepo = "https://github.com/microsoft/playwright"
