package browsers

// searchRegistry reads Windows App Paths registrations
// (HKLM/HKCU ...\App Paths\chrome.exe etc), which is where properly installed
// browsers register their real executable regardless of where the user put it.
func searchRegistry() []Found {
	return searchRegistryImpl()
}
