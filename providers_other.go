//go:build !windows

package main

// The detector is Windows-only. These stubs keep the non-Windows build (and
// unit tests for the pure logic) working on other platforms.
func scanLibraries(cfg Config, verbose bool) ([]LibraryGame, []string) {
	return nil, []string{"game detection is only supported on Windows"}
}

func steamRunningAppID() string {
	return ""
}
