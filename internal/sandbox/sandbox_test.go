package sandbox

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTestSandbox creates a temporary workdir with a user subdirectory
// and returns a Sandbox instance ready for testing.
func setupTestSandbox(t *testing.T) (*Sandbox, func()) {
	t.Helper()

	workDir := t.TempDir()
	userDir := filepath.Join(workDir, "testuser")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatalf("failed to create user dir: %v", err)
	}

	queue := make(chan struct{}, 1)
	s := New(workDir, "testuser", queue)

	cleanup := func() {
		// t.TempDir() cleans up automatically; nothing extra needed
	}

	return s, cleanup
}

// requireBwrap skips the test if bwrap is not available on the system.
func requireBwrap(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not available on this system; skipping sandbox execution test")
	}
}

// testContext returns a context with a generous timeout to prevent tests
// from hanging indefinitely.
func testContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ---------------------------------------------------------------------------
// validateUserID tests (security: path traversal prevention)
// ---------------------------------------------------------------------------

func TestValidateUserID(t *testing.T) {
	tests := []struct {
		name    string
		userID  string
		wantErr bool
	}{
		// 正常系
		{"simple numeric", "123456789", false},
		{"alphanumeric", "user123", false},
		{"with hyphens", "user-name", false},
		{"with underscores", "user_name", false},
		{"with dots", "user.name", false},
		{"mixed", "abc-123_def.456", false},

		// 拒否されるべきケース（パストラバーサル）
		{"empty string", "", true},
		{"parent dir only", "..", true},
		{"parent dir prefix", "../etc", true},
		{"parent dir suffix", "foo/..", true},
		{"nested traversal", "foo/../../etc", true},
		{"absolute path", "/etc/passwd", true},
		{"backslash separator", "..\\etc", true},
		{"windows drive", "C:\\Users", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUserID(tc.userID)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateUserID(%q) error = %v; wantErr = %v", tc.userID, err, tc.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Queue serialization tests (require bwrap)
// ---------------------------------------------------------------------------

// TestQueueSerialization verifies that two concurrent Exec calls are serialized:
// the second call must wait for the first to complete.
func TestQueueSerialization(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Use a channel to signal that the first command has started its execution.
	// We detect this by having the goroutine close the channel right before
	// calling Exec, then the main goroutine waits briefly for the queue acquire.
	firstStarted := make(chan struct{})

	go func() {
		defer wg.Done()
		close(firstStarted)
		_, err := s.Exec(ctx, "sleep 0.5")
		if err != nil {
			t.Errorf("first Exec failed: %v", err)
		}
	}()

	// Wait for the goroutine to start, then give it time to acquire the queue
	<-firstStarted
	time.Sleep(50 * time.Millisecond)

	// Start the second command and measure how long it takes
	start := time.Now()
	var secondOutput string
	var secondErr error

	go func() {
		defer wg.Done()
		secondOutput, secondErr = s.Exec(ctx, "echo done")
	}()

	wg.Wait()
	elapsed := time.Since(start)

	if secondErr != nil {
		t.Fatalf("second Exec failed: %v", secondErr)
	}
	if !strings.Contains(secondOutput, "done") {
		t.Errorf("second output expected 'done', got: %s", secondOutput)
	}

	// The second command should have waited for the first (sleep 0.5).
	// If serialized correctly, the second command's wait time should be
	// at least ~450ms (500ms sleep - 50ms head start).
	if elapsed < 400*time.Millisecond {
		t.Errorf("expected second command to wait for first (>=400ms), got %v", elapsed)
	}
}

// TestQueueTimeout verifies that if the queue is full and the context expires,
// Exec returns the context error instead of blocking indefinitely.
func TestQueueTimeout(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	// Pre-fill the queue to simulate another command running
	s.queue <- struct{}{}

	// Create a context that expires quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := s.Exec(ctx, "echo should-not-run")

	if err == nil {
		t.Fatal("expected error due to context timeout, got nil")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got: %v", err)
	}

	// Drain the queue so the test doesn't leak goroutines
	<-s.queue
}

// TestQueueReleaseOnError verifies that when a command fails, the queue is
// still released so the next call can proceed immediately.
func TestQueueReleaseOnError(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Run a command that fails (non-zero exit code)
	_, err1 := s.Exec(ctx, "exit 1")
	if err1 == nil {
		t.Error("expected error from 'exit 1', got nil")
	}

	// The queue should have been released. The next command should succeed.
	output, err2 := s.Exec(ctx, "echo queue-released")
	if err2 != nil {
		t.Fatalf("second Exec after error should succeed, got: %v", err2)
	}
	if !strings.Contains(output, "queue-released") {
		t.Errorf("expected output to contain 'queue-released', got: %s", output)
	}
}

// TestQueueReleaseOnInvalidUserID verifies that the queue is NOT acquired
// when the userID is invalid (validation happens before queue acquisition).
func TestQueueReleaseOnInvalidUserID(t *testing.T) {
	// This test does NOT require bwrap because validation happens first.

	queue := make(chan struct{}, 1)
	s := New("/tmp", "..", queue)

	// Pre-fill the queue to verify it's never touched
	queue <- struct{}{}

	_, err := s.Exec(context.Background(), "echo should-not-run")
	if err == nil {
		t.Fatal("expected error for invalid userID, got nil")
	}
	if !strings.Contains(err.Error(), "invalid userID") {
		t.Errorf("expected 'invalid userID' error, got: %v", err)
	}

	// The queue should still be full (validation rejected before queue acquire)
	if len(queue) != 1 {
		t.Error("expected queue to remain full (validation happened before queue acquire)")
	}

	// Drain the queue
	<-queue
}

// ---------------------------------------------------------------------------
// bwrap execution tests
// ---------------------------------------------------------------------------

// TestBwrapSimpleCommand verifies basic command execution works.
func TestBwrapSimpleCommand(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	output, err := s.Exec(ctx, "echo hello")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

// TestBwrapPipeSupport verifies that bash pipe syntax works inside the sandbox.
func TestBwrapPipeSupport(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	output, err := s.Exec(ctx, "echo hello | grep hello")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

// TestBwrapRedirectSupport verifies that bash redirect syntax works inside the sandbox.
func TestBwrapRedirectSupport(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	output, err := s.Exec(ctx, "echo hello > test.txt && cat test.txt")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", output)
	}
}

// TestBwrapNetworkDenied verifies that network access is blocked inside the sandbox.
// The sandbox uses --unshare-all which creates an isolated network namespace.
func TestBwrapNetworkDenied(t *testing.T) {
	requireBwrap(t)

	// Check that curl is available on the host (it should be inside the sandbox too
	// since /usr is mounted read-only). If curl is not available, skip the test.
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available on this system; skipping network isolation test")
	}

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// curl should fail due to network isolation; the || fallback should print BLOCKED
	output, err := s.Exec(ctx, "curl --max-time 3 http://google.com 2>&1 || echo BLOCKED")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "BLOCKED") {
		t.Errorf("expected output to contain 'BLOCKED' (network should be denied), got: %s", output)
	}
}

// TestBwrapEnvironmentPATH verifies that the PATH environment variable is set correctly.
func TestBwrapEnvironmentPATH(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	output, err := s.Exec(ctx, "echo $PATH")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "/usr/bin") {
		t.Errorf("expected PATH to contain '/usr/bin', got: %s", output)
	}
}

// TestBwrapEnvironmentHOME verifies that the HOME environment variable is set to /workdir.
func TestBwrapEnvironmentHOME(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	output, err := s.Exec(ctx, "echo $HOME")
	if err != nil {
		t.Fatalf("Exec failed: %v", err)
	}
	if !strings.Contains(output, "/workdir") {
		t.Errorf("expected HOME to contain '/workdir', got: %s", output)
	}
}

// TestBwrapOutputTruncation verifies that output exceeding 1MB is truncated.
func TestBwrapOutputTruncation(t *testing.T) {
	requireBwrap(t)

	// Check that yes is available
	if _, err := exec.LookPath("yes"); err != nil {
		t.Skip("yes not available on this system; skipping truncation test")
	}

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Generate ~2MB of output (yes outputs "y\n" repeatedly).
	// Use || true to avoid SIGPIPE exit status 141 from the pipe breaking.
	output, err := s.Exec(ctx, "yes | head -c 2000000 || true")

	// When systemd-run is used, the pipe may be closed before all data
	// is read, resulting in "read/write on closed pipe". In that case the
	// output is embedded in the error message. Check both.
	combined := output
	if err != nil {
		combined = err.Error()
		t.Logf("Exec returned error (may be expected with systemd-run): %v", err)
	}

	// The output should contain the truncation message
	if !strings.Contains(combined, "truncated") {
		t.Error("expected output to contain truncation message")
	}

	// The output should be truncated to approximately 1MB + truncation message.
	// Allow some tolerance for the trailing newline from "yes".
	const maxOutputSize = 1 << 20 // 1MB
	truncMsg := "\n... [output truncated at 1MB]"
	maxExpected := maxOutputSize + len(truncMsg) + 1 // +1 for trailing newline tolerance
	if len(output) > maxExpected {
		t.Errorf("output size %d exceeds max expected %d", len(output), maxExpected)
	}
	// Output should definitely be less than 2MB (the raw output size)
	if len(output) > 2*maxOutputSize {
		t.Errorf("output size %d is way too large (should be truncated)", len(output))
	}
}

// ---------------------------------------------------------------------------
// Edge case: context cancellation during execution
// ---------------------------------------------------------------------------

// TestBwrapContextCancel verifies that a command is killed when the context is cancelled.
func TestBwrapContextCancel(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := s.Exec(ctx, "sleep 10")
		errCh <- err
	}()

	// Give the command time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context
	cancel()

	// The command should fail when the context is cancelled.
	// Go's exec.CommandContext sends SIGKILL, which may surface as
	// "signal: killed" rather than a context error.
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancellation, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "context canceled") &&
			err != context.Canceled &&
			!strings.Contains(errMsg, "killed") &&
			!strings.Contains(errMsg, "signal") {
			t.Errorf("expected cancellation/kill error, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cancelled command to return")
	}
}

// ---------------------------------------------------------------------------
// Edge case: empty command
// ---------------------------------------------------------------------------

// TestBwrapEmptyCommand verifies that an empty command produces an error.
func TestBwrapEmptyCommand(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	_, err := s.Exec(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

// ---------------------------------------------------------------------------
// Edge case: very long command string
// ---------------------------------------------------------------------------

// TestBwrapLongCommand verifies that a very long command string is handled.
func TestBwrapLongCommand(t *testing.T) {
	requireBwrap(t)

	s, cleanup := setupTestSandbox(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Build a long command that just echoes a string
	longStr := strings.Repeat("a", 10000)
	cmd := "echo " + longStr

	output, err := s.Exec(ctx, cmd)
	if err != nil {
		t.Fatalf("Exec failed for long command: %v", err)
	}
	if !strings.Contains(output, longStr[:100]) {
		t.Error("expected output to contain the beginning of the long string")
	}
}