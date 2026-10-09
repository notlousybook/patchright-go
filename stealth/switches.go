package stealth

import (
	"strings"

	"github.com/notlousybook/patchright-go/patcher"
)

// FilterChromiumSwitchesForLaunch applies the chromiumSwitchesPatch policy to
// runtime launch-arg lists (bare `--flag` strings, as Playwright's
// BrowserTypeLaunchOptions.Args carries them). Flags the driver patch removes
// are dropped; the AutomationControlled blink-feature disable is appended.
func FilterChromiumSwitchesForLaunch(args []string) []string {
	disabled := map[string]bool{}
	for _, s := range patcher.SwitchesToDisable {
		flag := s
		if i := strings.Index(flag, "'--"); i >= 0 {
			flag = flag[i+1:]
			if j := strings.Index(flag, "'"); j >= 0 {
				flag = flag[:j]
			}
		}
		if strings.HasPrefix(flag, "--") {
			disabled[flag] = true
		}
	}
	out := make([]string, 0, len(args)+1)
	for _, a := range args {
		if disabled[a] {
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
