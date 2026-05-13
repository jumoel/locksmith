package ecosystem

import "path/filepath"

// fileExistsGlob returns true if any file matching the glob pattern exists.
// Used for musl detection in runtimeLibc; isolated here to keep the
// architectures.go file portable and easy to mock in tests if needed later.
func fileExistsGlob(pattern string) bool {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false
	}
	return len(matches) > 0
}
