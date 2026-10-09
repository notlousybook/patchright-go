// Package patchrightgo is undetected playwright, but in go.
//
// patchright takes stock microsoft/playwright and applies 47 stealth
// transforms to the driver: no Runtime.enable, no __pw_* globals, init
// scripts injected through request routing, automation flags stripped from
// chromium, closed shadow roots pierced, all that.
//
// start with client if you just wanna drive a browser, patcher if you wanna
// build the driver yourself, browsers if you need to find one, and demo if
// you wanna see it all work. check the README, it explains everything.
//
// all the stealth engineering is Vinyzu's (github.com/Kaliiiiiiiiii-Vinyzu/patchright),
// this is just the go translation.
package patchrightgo
