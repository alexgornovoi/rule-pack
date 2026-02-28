package main

import "strings"

// buildVersion is intended to be overridden at build time using:
// -ldflags "-X main.buildVersion=vX.Y.Z"
var buildVersion = "dev"

func appVersion() string {
	v := strings.TrimSpace(buildVersion)
	if v == "" {
		return "dev"
	}
	return v
}
