package stealth

import (
	"strings"

	"github.com/notlousybook/patchright-go/patcher"
)

// FilterChromiumSwitchesForLaunch applies the chromiumSwitchesPatch policy to
// runtime launch-arg lists (bare `--flag` strings, as Playwright's
// BrowserTypeLaunchOptions.Args carries them). Flags the driver patch removes
// are dropped; the AutomationControlled blink-feature disable is appended.
//
// The flag set is owned by patcher.RemovedSwitchFlags — this function only
// applies it, it does not carry its own copy.
func FilterChromiumSwitchesForLaunch(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if isDisabledFlag(a) {
			continue
		}
		out = append(out, a)
	}
	stealth := strings.Trim(patcher.StealthSwitch, "'")
	for _, a := range out {
		if a == stealth {
			return out
		}
	}
	return append(out, stealth)
}

func isDisabledFlag(flag string) bool {
	for _, f := range patcher.RemovedSwitchFlags {
		if f == flag {
			return true
		}
	}
	return false
}
