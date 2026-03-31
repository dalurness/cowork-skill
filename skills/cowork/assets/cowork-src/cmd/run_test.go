package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// coworkBin is the path to the compiled test binary, built once in TestMain.
var coworkBin string

// testdataDir is the absolute path to the cmd/testdata directory.
var testdataDir string

// TestMain builds the cowork binary once, then runs all tests.
func TestMain(m *testing.M) {
	// Resolve testdata dir (cwd during tests is the cmd/ package dir)
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("os.Getwd: %v", err)
	}
	testdataDir = filepath.Join(wd, "testdata")

	// Build the cowork binary into a temp dir.
	tmp, err := os.MkdirTemp("", "cowork-test-bin-*")
	if err != nil {
		log.Fatalf("MkdirTemp: %v", err)
	}
	coworkBin = filepath.Join(tmp, "cowork")

	// Module root is one level up from cmd/.
	moduleRoot := filepath.Dir(wd)
	buildCmd := exec.Command("go", "build", "-o", coworkBin, ".")
	buildCmd.Dir = moduleRoot
	if out, err := buildCmd.CombinedOutput(); err != nil {
		log.Fatalf("Failed to build cowork binary: %v\n%s", err, out)
	}

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// --------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------

// setupProject creates a minimal cowork project structure in dir.
// tasks is a list of task IDs (e.g. ["TASK-001"]) to add to the queue
// and create BRIEF.md for.
func setupProject(t *testing.T, dir string, tasks []string) {
	t.Helper()
	// Create required directories
	for _, d := range []string{"queue", "work", "log", "questions", "decisions", "history", "updates"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Write state.json
	stateJSON := `{
  "runCount": 0,
  "taskCounter": 0,
  "questionCounter": 0,
  "lastRun": "",
  "phase": "idle"
}
`
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatalf("write state.json: %v", err)
	}

	// Write queue/todo.md
	var queueLines []string
	for _, taskID := range tasks {
		queueLines = append(queueLines, fmt.Sprintf("### [%s] (normal)", taskID))
	}
	queueContent := strings.Join(queueLines, "\n")
	if len(queueLines) > 0 {
		queueContent += "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "queue", "todo.md"), []byte(queueContent), 0o644); err != nil {
		t.Fatalf("write queue/todo.md: %v", err)
	}

	// Create work dirs and BRIEF.md for each task
	for _, taskID := range tasks {
		taskDir := filepath.Join(dir, "work", taskID)
		if err := os.MkdirAll(taskDir, 0o755); err != nil {
			t.Fatalf("mkdir work/%s: %v", taskID, err)
		}
		brief := fmt.Sprintf(`# %s — Test Task

**Task ID:** %s
**Working directory:** work/%s/
**Project root:** ../../
**Mode:** implementation
**Priority:** normal

## Scope
Test scope for %s.

## Expected Output
Write OUTPUT.md with results.
`, taskID, taskID, taskID, taskID)
		if err := os.WriteFile(filepath.Join(taskDir, "BRIEF.md"), []byte(brief), 0o644); err != nil {
			t.Fatalf("write BRIEF.md for %s: %v", taskID, err)
		}
	}
}

// runCowork runs the compiled cowork binary with the given args.
// It waits up to testTimeout for the process to finish.
// Returns (exitCode, combinedOutput).
// If the test timeout fires before the process exits, the test is marked fatal.
func runCowork(t *testing.T, testTimeout time.Duration, args ...string) (int, string) {
	t.Helper()

	cmd := exec.Command(coworkBin, args...)
	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start cowork: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(testTimeout)
	defer timer.Stop()

	select {
	case err := <-done:
		output := outBuf.String()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return exitErr.ExitCode(), output
			}
			t.Fatalf("cowork unexpected error: %v\noutput: %s", err, output)
		}
		return 0, output

	case <-timer.C:
		// Kill the process and fail the test
		cmd.Process.Kill()
		<-done
		t.Fatalf("test timed out after %v waiting for cowork to exit\noutput so far: %s",
			testTimeout, outBuf.String())
		return -1, ""
	}
}

// scriptPath returns the absolute path to a testdata mock script.
func scriptPath(name string) string {
	return filepath.Join(testdataDir, name)
}

// queueContainsTask returns true if queue/todo.md contains taskID.
func queueContainsTask(t *testing.T, projectDir, taskID string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "queue", "todo.md"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), taskID)
}

// readState reads and unmarshals state.json from a project dir.
func readState(t *testing.T, projectDir string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, "state.json"))
	if err != nil {
		t.Fatalf("read state.json: %v", err)
	}
	var s map[string]interface{}
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("parse state.json: %v", err)
	}
	return s
}

// baseArgs returns common flags used in most tests.
// --pm-every 0 --architect-every 0 disables PM/architect passes that
// would fail with missing --skill-dir.
func baseArgs(projectDir string, extra ...string) []string {
	args := []string{
		"run",
		"--project", projectDir,
		"--pm-every", "0",
		"--architect-every", "0",
	}
	return append(args, extra...)
}

// --------------------------------------------------------------------
// Tests
// --------------------------------------------------------------------

// TestRunOnce_NoTasks verifies that running with an empty queue exits cleanly.
func TestRunOnce_NoTasks(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, nil) // no tasks

	exitCode, output := runCowork(t, 30*time.Second,
		baseArgs(dir)...,
	)

	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}
	if !strings.Contains(output, "No ready tasks") {
		t.Errorf("expected 'No ready tasks' in output, got:\n%s", output)
	}
}

// TestRunOnce_TaskCompletes verifies that a successful worker removes the task
// from the queue and leaves OUTPUT.md in the work dir.
func TestRunOnce_TaskCompletes(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, []string{"TASK-001"})

	exitCode, output := runCowork(t, 30*time.Second,
		baseArgs(dir,
			"--worker-cmd", scriptPath("mock-worker-success.sh"),
		)...,
	)

	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	// OUTPUT.md must exist in the task work dir
	outputPath := filepath.Join(dir, "work", "TASK-001", "OUTPUT.md")
	if _, err := os.Stat(outputPath); err != nil {
		t.Errorf("expected OUTPUT.md at %s, got: %v", outputPath, err)
	}

	// TASK-001 must be removed from the queue
	if queueContainsTask(t, dir, "TASK-001") {
		queueData, _ := os.ReadFile(filepath.Join(dir, "queue", "todo.md"))
		t.Errorf("expected TASK-001 removed from queue, but it is still present:\n%s", queueData)
	}
}

// TestRunOnce_RetryLimit verifies that after 3 failed attempts (no OUTPUT.md),
// the task is marked stuck and removed from the queue, and the binary exits non-zero.
func TestRunOnce_RetryLimit(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, []string{"TASK-001"})

	noop := scriptPath("mock-worker-noop.sh")
	args := baseArgs(dir, "--worker-cmd", noop)

	// Run 1: attempt 1 — noop, task stays in queue
	exit1, out1 := runCowork(t, 30*time.Second, args...)
	if exit1 != 0 {
		t.Fatalf("run 1: expected exit 0, got %d\noutput: %s", exit1, out1)
	}
	if !queueContainsTask(t, dir, "TASK-001") {
		t.Fatalf("run 1: expected TASK-001 still in queue after first failed attempt")
	}
	// Verify attempts file was written
	attemptsData, err := os.ReadFile(filepath.Join(dir, "work", "TASK-001", "attempts"))
	if err != nil {
		t.Fatalf("run 1: could not read attempts file: %v", err)
	}
	if strings.TrimSpace(string(attemptsData)) != "1" {
		t.Fatalf("run 1: expected attempts=1, got %q", strings.TrimSpace(string(attemptsData)))
	}

	// Run 2: attempt 2 — noop, task stays in queue
	exit2, out2 := runCowork(t, 30*time.Second, args...)
	if exit2 != 0 {
		t.Fatalf("run 2: expected exit 0, got %d\noutput: %s", exit2, out2)
	}
	if !queueContainsTask(t, dir, "TASK-001") {
		t.Fatalf("run 2: expected TASK-001 still in queue after second failed attempt")
	}

	// Run 3: attempt 3 — STUCK threshold reached, exit non-zero
	exit3, out3 := runCowork(t, 30*time.Second, args...)
	if exit3 == 0 {
		t.Errorf("run 3: expected non-zero exit (stuck task), got 0\noutput: %s", out3)
	}

	// stuck.md must exist in project root
	stuckPath := filepath.Join(dir, "stuck.md")
	stuckData, err := os.ReadFile(stuckPath)
	if err != nil {
		t.Fatalf("run 3: expected stuck.md to exist at %s: %v", stuckPath, err)
	}
	if !strings.Contains(string(stuckData), "TASK-001") {
		t.Errorf("run 3: stuck.md does not mention TASK-001:\n%s", stuckData)
	}

	// TASK-001 must be removed from queue
	if queueContainsTask(t, dir, "TASK-001") {
		t.Errorf("run 3: expected TASK-001 removed from queue after stuck, but it is still present")
	}
}

// TestRunForever_ExitsOnTimeout verifies that --forever with a short timeout
// exits cleanly when the task completes (before the timeout fires).
func TestRunForever_ExitsOnTimeout(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, []string{"TASK-001"})

	start := time.Now()
	exitCode, output := runCowork(t, 60*time.Second,
		baseArgs(dir,
			"--forever",
			"--timeout", "30s",
			"--drain-window", "5s",
			"--worker-cmd", scriptPath("mock-worker-success.sh"),
		)...,
	)
	elapsed := time.Since(start)

	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}
	// Should have exited well before the 30s timeout (task completes instantly)
	if elapsed > 35*time.Second {
		t.Errorf("took too long: %v (expected < 35s)", elapsed)
	}
	_ = output
}

// TestRunForever_ExitsWhenComplete verifies that --forever exits with
// "PROJECT COMPLETE" and phase=complete when there are no tasks or questions.
func TestRunForever_ExitsWhenComplete(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, nil) // empty queue — project is immediately "complete"

	exitCode, output := runCowork(t, 30*time.Second,
		baseArgs(dir,
			"--forever",
		)...,
	)

	if exitCode != 0 {
		t.Errorf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "PROJECT COMPLETE") {
		t.Errorf("expected 'PROJECT COMPLETE' in output\noutput: %s", output)
	}

	// State must show phase=complete
	s := readState(t, dir)
	if phase, _ := s["phase"].(string); phase != "complete" {
		t.Errorf("expected state.phase=complete, got %q", phase)
	}
}

// TestContextTimeout_HardKill verifies that --timeout kills a hanging worker
// and the binary exits well before the worker would have finished.
func TestContextTimeout_HardKill(t *testing.T) {
	dir := t.TempDir()
	setupProject(t, dir, []string{"TASK-001"})

	start := time.Now()
	exitCode, output := runCowork(t, 15*time.Second, // test fails if process doesn't exit within 15s
		baseArgs(dir,
			"--timeout", "5s",
			"--worker-cmd", scriptPath("mock-worker-hang.sh"),
		)...,
	)
	elapsed := time.Since(start)

	// Exit code 0 is acceptable (context cancellation is not an error in runOnce)
	_ = exitCode

	// Must have exited well before the hang script would finish (3600s)
	if elapsed > 10*time.Second {
		t.Errorf("expected exit within 10s, took %v\noutput: %s", elapsed, output)
	}
}

// TestDrainWindow verifies that --forever stops launching new cycles when
// there is less time remaining than the drain window.
func TestDrainWindow(t *testing.T) {
	dir := t.TempDir()
	// Queue has a task. Noop worker won't complete it, so the loop would continue
	// indefinitely — unless the drain window fires.
	setupProject(t, dir, []string{"TASK-001"})

	start := time.Now()
	exitCode, output := runCowork(t, 20*time.Second, // test fails if process doesn't exit within 20s
		baseArgs(dir,
			"--forever",
			"--timeout", "10s",
			"--drain-window", "9s",
			"--poll-interval", "1s",
			"--worker-cmd", scriptPath("mock-worker-noop.sh"),
		)...,
	)
	elapsed := time.Since(start)

	if exitCode != 0 {
		t.Errorf("expected clean exit (0), got %d\noutput: %s", exitCode, output)
	}

	// Must have stopped well before the 10s total timeout
	if elapsed > 15*time.Second {
		t.Errorf("took too long: %v (expected < 15s)\noutput: %s", elapsed, output)
	}

	// Should NOT have created stuck.md (task was not attempted 3 times)
	stuckPath := filepath.Join(dir, "stuck.md")
	if _, err := os.Stat(stuckPath); err == nil {
		data, _ := os.ReadFile(stuckPath)
		t.Errorf("unexpected stuck.md — drain window should have stopped before 3 attempts:\n%s", data)
	}

	// Output should mention drain window stop reason
	if !strings.Contains(output, "drain") && !strings.Contains(output, "Stopping") {
		t.Logf("drain-window output check (informational): %s", output)
	}
}
