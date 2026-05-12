//go:build integration

package testharness

import (
	"os/exec"
	"time"
)

// runDockerWithNetworkRetry executes a `docker run ...` invocation and retries
// up to maxAttempts times when the output looks like a transient network
// failure. Returns the final output, the final error (nil on success), and
// the attempt count so callers can log when a retry was needed.
//
// What counts as transient is decided by isTransientNetworkError; real
// assertion failures (checksum mismatches, unresolvable peer deps, etc.)
// don't match and fail immediately.
//
// We retry only the Docker invocation, not anything that ran before it.
// Each retry rebuilds the *exec.Cmd because exec.Cmd is not reusable.
func runDockerWithNetworkRetry(dockerArgs []string, maxAttempts int) ([]byte, error, int) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	var lastOutput []byte
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		cmd := exec.Command("docker", dockerArgs...)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return out, nil, attempt
		}
		lastOutput, lastErr = out, err
		if !isTransientNetworkError(string(out)) {
			// Genuine failure - don't retry.
			return out, err, attempt
		}
		if attempt < maxAttempts {
			// Exponential backoff: 1s, 2s, 4s.
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}
	}
	return lastOutput, lastErr, maxAttempts
}
