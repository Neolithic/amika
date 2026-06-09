//go:build !linux && !darwin

package eventlog

// flockExclusive is a no-op on platforms without flock(2). amikalog is built
// and released only for linux and darwin (see install.sh and the release
// workflow); this keeps the package compiling for other GOOS values, e.g. a
// local `go build` on Windows.
func flockExclusive(uintptr) error { return nil }
