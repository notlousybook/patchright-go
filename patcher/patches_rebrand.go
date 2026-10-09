package patcher

// This file ports cliAliasPatch.ts verbatim: Playwright -> Patchright CLI
// string replacements across cli/program.ts, cli/programWithTestStub.ts,
// server/android/android.ts and server/registry/index.ts.

var cliProgramReplacements = [][2]string{
	{"Playwright version:", "Patchright version:"},
	{"ensure browsers necessary for this version of Playwright are installed", "ensure browsers necessary for this version of Patchright are installed"},
	{"prints list of browsers from all playwright installations", "prints list of browsers from all patchright installations"},
	{"WARNING: It looks like you are running 'npx playwright install' without first", "WARNING: It looks like you are running 'npx patchright install' without first"},
	{"then run Playwright's install command:", "then run Patchright's install command:"},
	{"    npx playwright install", "    npx patchright install"},
	{"If your project does not yet depend on Playwright, first install the", "If your project does not yet depend on Patchright, first install the"},
	{"then run Playwright's install command to download the browsers:", "then run Patchright's install command to download the browsers:"},
	{"    npm install @playwright/test", "    npm install -D patchright"},
	{"Playwright Host validation warning", "Patchright Host validation warning"},
	{"Removes browsers used by this installation of Playwright from the system (chromium, firefox, webkit, ffmpeg). This does not include branded channels.", "Removes browsers used by this installation of Patchright from the system (chromium, firefox, webkit, ffmpeg). This does not include branded channels."},
	{"Removes all browsers used by any Playwright installation from the system.", "Removes all browsers used by any Patchright installation from the system."},
	{"Successfully uninstalled Playwright browsers for the current Playwright installation.", "Successfully uninstalled Patchright browsers for the current Patchright installation."},
	{"used by other Playwright installations.\\nTo uninstall Playwright browsers for all installations, re-run with --all flag.", "used by other Patchright installations.\\nTo uninstall Patchright browsers for all installations, re-run with --all flag."},
	{"timeout for Playwright actions in milliseconds, no timeout by default", "timeout for Patchright actions in milliseconds, no timeout by default"},
	{"return `playwright`;", "return `patchright`;"},
	{"return `${packageManagerCommand} playwright`;", "return `${packageManagerCommand} patchright`;"},
}

var cliProgramWithTestStubReplacements = [][2]string{
	{"yarn playwright", "yarn patchright"},
	{"pnpm exec playwright", "pnpm exec patchright"},
	{"npx playwright", "npx patchright"},
	{"Run tests with Playwright Test.", "Run tests with Patchright Test."},
	{"Show Playwright Test HTML report.", "Show Patchright Test HTML report."},
	{"Merge Playwright Test Blob reports", "Merge Patchright Test Blob reports"},
}

var androidReplacements = [][2]string{
	{"playwright install android", "patchright install android"},
}

var registryReplacements = [][2]string{
	{"Looks like ${sdkLanguage === 'javascript' ? 'Playwright Test or ' : ''}Playwright was just updated to ${preferredDockerVersion.driverVersion}.", "Looks like ${sdkLanguage === 'javascript' ? 'Patchright Test or ' : ''}Patchright was just updated to ${preferredDockerVersion.driverVersion}."},
	{"Looks like ${sdkLanguage === 'javascript' ? 'Playwright Test or ' : ''}Playwright was just installed or updated.", "Looks like ${sdkLanguage === 'javascript' ? 'Patchright Test or ' : ''}Patchright was just installed or updated."},
	{"<3 Playwright Team", "<3 Patchright Team"},
	{"ERROR: Playwright does not support installing ${executable.name}", "ERROR: Patchright does not support installing ${executable.name}"},
	{"wait a few minutes if other Playwright is installing browsers in parallel", "wait a few minutes if other Patchright is installing browsers in parallel"},
	{"ERROR: Playwright does not support ${descriptor.name} on ${hostPlatform}", "ERROR: Patchright does not support ${descriptor.name} on ${hostPlatform}"},
	{"BEWARE: your OS is not officially supported by Playwright; downloading fallback build for ${hostPlatform}.", "BEWARE: your OS is not officially supported by Patchright; downloading fallback build for ${hostPlatform}."},
	{"::warning title=Playwright::${message}", "::warning title=Patchright::${message}"},
	{"return `playwright ${parameters}`;", "return `patchright ${parameters}`;"},
}

// RebrandTables exposes the tables for the client package's rebrand helper.
var RebrandTables = map[string][][2]string{
	"packages/playwright-core/src/cli/program.ts":             cliProgramReplacements,
	"packages/playwright-core/src/cli/programWithTestStub.ts": cliProgramWithTestStubReplacements,
	"packages/playwright-core/src/server/android/android.ts":  androidReplacements,
	"packages/playwright-core/src/server/registry/index.ts":   registryReplacements,
}

func init() {
	register(PatchFunc{
		Name: "patchCliAlias",
		Apply: func(fs *FileSet) error {
			for rel, pairs := range RebrandTables {
				text := fs.MustGet(rel)
				fs.Set(rel, ReplaceAll(text, pairs))
			}
			return nil
		},
	})
}
