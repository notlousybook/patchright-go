# patchright-go

undetected playwright, but make it's actually cool and good (golang edition)

so uhhh yeah. [patchright](https://github.com/Kaliiiiiiiiii-Vinyzu/patchright) is cool it's a stealth-patched playwright driver that doesn't set off every bot detector on the planet. problem is that uhh the go port of it is ass and prob outdated i was too lazy to check, uhh so i forced my ai to port it entirely (i hope) to go

> made cuz the other one was outdated and also i wanted to and i need it for other stuffs

## what is this

patchright takes stock `microsoft/playwright` and applies 47 stealth transforms to the driver source: no `Runtime.enable`, no `__pw_*` globals leaking into pages, init scripts injected through request routing instead of detectable CDP calls, automation fingerprint flags stripped from chromium, closed shadow roots pierced via CDP fallbacks, the whole deal. this repo is that entire pipeline, rewritten in go.

nothing is pinned btw. `patch` just grabs the latest playwright release unless you tell it otherwise. i hate pinned versions.

#### oh and btw, about the driver version

playwright-go v0.6000.0 hard-codes driver CLI **1.60.0** and refuses to launch anything else (its `isUpToDateDriver` checks the printed version). so `driver.Version` is v1.60.0, matching upstream's `patchright-core` npm publishing. bump the binding, bump that, and check `npm view patchright-core` has your version.

> only chromium-based browsers are patched, same as upstream. firefox and webkit are not supported. sorry brah

## quick start

```bash
# patch a playwright checkout (grabs latest by default, --version vX.Y.Z if ur feeling specific)
go run ./cmd/patchright patch --playwright-dir ./playwright

# then build the driver like normal
cd playwright && npm ci && npm run build
```

or skip all that and just drive a browser:

```go
import (
    // NOTE: stay on the playwright-community import path. newer playwright-go
    // tags (v0.61+) flipped back to github.com/mxschmitt/playwright-go which
    // breaks every import for no reason. v0.6000.0 works fine against newer
    // drivers, the client wrapper doesn't care about the binding version.
    pw "github.com/playwright-community/playwright-go"
    "github.com/notlousybook/patchright-go/browsers"
    "github.com/notlousybook/patchright-go/client"
)

inst, _ := client.Run() // provisions the patched driver on first use
defer inst.Stop()

found := browsers.Default() // sniffs out your most used chromium install
ctx, page, _ := inst.LaunchStealth(found, false) // one seam: selection + route + page
page.Goto("https://example.com")
page.AddInitScript(pw.Script{Content: pw.String("window.cool = true")})
```

or even lazier, just run the demo:

```bash
go run ./demo                 # finds your browser, launches it, shows stealth working
go run ./demo -browser brave  # no, THIS one
go run ./demo -headless       # no window
go run ./demo -url https://example.com  # also check a live page
```

## one mode: full stealth, zero setup

there's no manual patch step and no stock fallback. `client.Run()` auto-provisions the patched driver on first use and caches it per version. zero npm, zero node toolchain, zero brain cells:

```go
inst, _ := client.Run() // that's it. patched driver, full stealth.
defer inst.Stop()
```

### how the driver gets patched (no npm, trust me)

two tiers, tried in order, first one that verifies wins:

1. **npm overlay** ← the normal path: download the stock driver zip from playwright's CDN for the bundled node runtime, then overlay upstream's own `patchright-core` npm package on top of it. that package IS Vinyzu's CI output of the full 47-patch build, so this is real full stealth, not our interpretation of it
2. **npm-only** — CDN outage? the `patchright-core` tarball alone is enough, and we fetch **node ourselves** from nodejs.org (live-tested: download → extract → `node --version` proves out)

if both fail you get a clear error. there's deliberately no "partial stealth" fallback: a half-patched driver that looks like a full one is worse than a loud failure. `driver.Source(dir).Describe()` tells you which tier you're on.

```go
inst, _ := client.Run()
defer inst.Stop()
```

### does the client pc need node? no.

every runtime dependency resolves itself:

- **normal case** (playwright CDN up): the stock driver zip we pull already **bundles node** with it. nothing to install.
- **CDN outage case**: we provision from the `patchright-core` npm tarball alone, then grab **node ourselves** from nodejs.org (live-tested: download → extract → `node --version` proves out). playwright-go is pointed at it via `PLAYWRIGHT_NODEJS_PATH`.

so a user with a bare `go run` machine and no node installed still gets full stealth. `driver.NodeSource()` tells you which one you got: `bundled`, `system:<path>`, or the dir it downloaded into.

```bash
go run ./demo                 # tells you stealth source + node source
go run ./demo -check-only     # no browser needed
```

## parity with upstream patchright — honest status

**full parity: driver-level stealth.** the runtime driver is upstream's *own* `patchright-core` build, so the patches doing the work are Vinyzu's, not a reimplementation.

**one mode.** `client.Run()` provisions a patched driver or errors — there's no wrapper-only mode and no partial-stealth fallback, by design.

**restored parity: isolated-world evaluate.** `client.EvaluateIsolated(page, fn, arg)` runs in a dedicated `Page.createIsolatedWorld` context via `Runtime.callFunctionOn`, which is what patchright's python/nodejs clients do by default. the playwright-go binding can't express the `isolatedContext` flag, so we reach it through the CDP seam instead of forking the binding. `EvaluateHandleIsolated` returns the raw CDP object id (documented limitation: pw's `JSHandle` is driver-backed, so a handle can't be fabricated from a CDP object).

**known gaps:**
- **driver version lags upstream.** pinned to **1.60.0** because playwright-go hard-codes its driver version and newer pw-go tags flipped their import path to `github.com/mxschmitt/...`. upstream patchright is further along. `driver` now *diagnoses* a stale pin instead of blaming the network, so a bump is deliberate rather than a silent regression.
- **client convenience surface** differs from `patchright-nodejs` (e.g. their `patchright.chromium.launch` shortcuts). `client.LaunchStealth` covers the launch path; not every convenience wrapper is mirrored.

## what's inside

| package | what it do |
|---|---|
| `patcher` | the full source-level patch pipeline (47 transforms over a playwright checkout, for building drivers yourself / CI). `driver/` is what normal users hit — same stealth, applied to the downloaded bundle instead of sources |
| `stealth` | the runtime sneaky bits as testable go code: CSP relaxation, `<head>` injection placement, launch-switch policy, `f[0-9a-f]{32}` bridge naming |
| `client` | the one mode + the one launch seam. `Run()` auto-provisions the patched driver; `LaunchStealth()` owns browser selection, switch policy, inject route and page; `EvaluateIsolated()` restores patchright's isolated-world evaluate default (no forked binding) |
| `driver` | self-provisioning patched driver: npm overlay of upstream's patched core, npm-only outage path (with node self-download). `driver.Ensure()` returns a `DriverDirectory` for playwright-go. fails loudly rather than serving partial stealth |
| `browsers` | finds every chromium on your machine (install dirs, start menu + desktop shortcuts, registry App Paths, PATH, flatpak, playwright's own bundle) on windows/mac/linux, picks the one you actually use |
| `demo` | the showcase. discovers browser, launches stealthy, dumps webdriver/globals/plugins/UA, proves init scripts + bindings + shadow DOM piercing work. reports which driver tier ran |
| `testmods` | rewrites upstream playwright specs (`isolatedContext=false` insertion, `fixme()` markers, about:blank workarounds) |
| `impact` | answers "did upstream break our patches": symbol table, diff parsing, change classification, markdown/JSON reports |
| `testutil` + `e2e` | detection smoke test + ported custom specs (behind `e2e` tag). the e2e harness logs driver provenance and warns loudly on partial stealth |
| `cmd/patchright` | the CLI. `patch` (build your own driver from source), `list`, `symbols`, `version-check`, `impact-check`, `modify-tests`, `rebrand`, `diff-patch`, `format` |

## the stealth stuff (so u know it actually works)

- **no `Runtime.enable` / `Console.enable`** — execution contexts get bootstrapped through `Runtime.evaluate({globalThis})` + `DOM.resolveNode` id parsing + `Page.createIsolatedWorld`. detector sees nothing.
- **route-based init scripts** — `addInitScript` / `exposeBinding` / `exposeFunction` install a `**/*` route, document requests fall back with a marker, and `Fetch.continueRequest{interceptResponse:true}` + `fulfill` injects the scripts into `<head>` with CSP fixes and self-removing tags.
- **chromium switches** — 13 snitch flags gone (`--enable-automation`, `--disable-popup-blocking`, ...), `--disable-blink-features=AutomationControlled` added, `--headless=new` forced.
- **closed shadow roots** — `DOM.describeNode(pierce)` + `DOM.resolveNode` fallback with dedup and DOM-order sorting, plus a closed-root XPath engine.
- **no `__pw_*` globals** — binding prefixes removed, bridges named `f` + 32 random hex. nothing to fingerprint.
- **protocol additions** — `isolatedContext?: boolean` on eval commands, `ContextOptions.focusControl`, `Route.continue.patchrightInitScript`.

known limitation (same as upstream, can't fix, don't @ me): init scripts don't run on `about:blank` / `data:` urls.

## CLI cheat sheet

```bash
go run ./cmd/patchright patch --playwright-dir ./playwright   # latest + patch
go run ./cmd/patchright patch --version v1.60.0                # specific one
go run ./cmd/patchright patch --only patchChromiumSwitches     # just one patch
go run ./cmd/patchright list                                   # all 47 patches
go run ./cmd/patchright symbols                                # patched-symbol manifest
go run ./cmd/patchright version-check --repo OWNER/REPO        # rebuild needed?
go run ./cmd/patchright impact-check --old-version v1.59.0 --new-version v1.60.0
go run ./cmd/patchright modify-tests --playwright-dir ./playwright
go run ./cmd/patchright rebrand --playwright-dir ./playwright  # playwright* -> patchright*
go run ./cmd/patchright diff-patch                             # doc-only patchright.patch
```

## testing

```bash
go test ./...                     # unit tests, no browser needed
go test -tags checkout ./patcher/ # replays all 47 patches vs real checkout
go test -tags e2e ./e2e/          # needs patched driver + chrome
```

## credit where credit's due

all the actual stealth engineering is by [Vinyzu](https://github.com/Kaliiiiiiiiii-Vinyzu) in the original [patchright](https://github.com/Kaliiiiiiiiii-Vinyzu/patchright) repo (and the [patchright-nodejs](https://github.com/Kaliiiiiiiiii-Vinyzu/patchright-nodejs) client). i just translated it to go cuz i needed it. upstream is apache 2.0, so is this.

## license

apache 2.0. go forth and be undetected 🫡


> ts human assisted repo ahh ✌️✌️😭
