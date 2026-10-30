// Package version carries the build's version, stamped by the linker:
//
//	go build -ldflags "-X github.com/fum4/waker/internal/version.Version=v1.2.3"
package version

import "runtime/debug"

// Version is set at link time; "dev" for plain go build.
var Version = "dev"

// String returns the stamped version, else the module version recorded by
// go install, else "dev".
func String() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}
