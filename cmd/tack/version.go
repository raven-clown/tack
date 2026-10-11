package main

import "runtime/debug"

// version is set by the release build; otherwise it comes from the module.
var version = "dev"

// versionString returns the version to show: the release tag, the module
// version `go install` recorded, or dev.
func versionString() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
