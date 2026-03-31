package cmd

import (
	"bufio"
	"context"
	"cowork/state"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// workerResult holds the outcome for a single worker task.
type workerResult struct {
	TaskID          string `json:"taskId"`
	TaskDir         string `json:"taskDir"`
	Status          string `json:"status"`
	ExitCode        int    `json:"exitCode"`
	ExitedAt        string `json:"exitedAt,omitempty"`
	OutputPresent   bool   `json:"outputPresent"`
	HandoffPresent  bool   `json:"handoffPresent"`
	ProgressEntries int    `json:"progressEntries"`
	Attempts        int    `json:"attempts,omitempty"`
}

// fullWorkerReport is the JSON report written after a worker run.
type fullWorkerReport struct {
	Status    string         `json:"status"`
	StartTime string         `json:"startTime"`
	EndTime   string         `json:"endTime"`
	QueueFile string         `json:"queueFile"`
	TaskIDs   []string       `json:"taskIds"`
	Workers   []workerResult `json:"workers"`
	Summary   struct {
		Total        int `json:"total"`
		Completed    int `json:"completed"`
		TimedOut     int `json:"timedOut"`
		Killed       int `json:"killed"`
		Failed       int `json:"failed"`
		NotStarted   int `json:"notStarted"`
		Stuck        int `json:"stuck"`
		SkippedDrain int `json:"skippedDrain"`
	} `json:"summary"`
}

// workerTimeout is the hard cap per worker session — not configurable.
const workerTimeout = time.Hour

// runWorkers runs the parallel worker pool directly (no external binary).
// Each worker is capped at workerTimeout (1 hour) regardless of other settings.
// ctx controls the overall run — if cancelled, all workers are killed.
// Returns the path to the JSON run report, or an error (including stuck-task errors).
func runWorkers(cmd *cobra.Command, projectDir string, ctx context.Context) (string, error) {
	skillDir, _ := cmd.Flags().GetString("skill-dir")
	maxWorkers, _ := cmd.Flags().GetInt("workers")
	queueFile, _ := cmd.Flags().GetString("queue")
	workDir, _ := cmd.Flags().GetString("work-dir")

	if queueFile == "" {
		queueFile = filepath.Join(projectDir, "queue", "todo.md")
	}
	if workDir == "" {
		workDir = filepath.Join(projectDir, "work")
	}

	// Build constructor prompt
	constructorPrompt, err := buildConstructorPrompt(skillDir, projectDir)
	if err != nil {
		return "", fmt.Errorf("building constructor prompt: %w", err)
	}

	// Read task IDs from queue
	taskIDs := readTaskIDs(queueFile)
	if len(taskIDs) == 0 {
		return "", fmt.Errorf("no tasks found in %s", queueFile)
	}

	// Prepare report path
	now := time.Now().UTC()
	logDir := filepath.Join(projectDir, "log")
	os.MkdirAll(logDir, 0o755)
	s, _ := state.Load(projectDir)
	runCount := 0
	if s != nil {
		runCount = s.RunCount
	}
	reportPath := filepath.Join(logDir, fmt.Sprintf("%s-run-%03d-complete.json",
		now.Format("2006-01-02T150405"), runCount))

	startTime := time.Now().UTC()

	var mu sync.Mutex
	results := make([]workerResult, len(taskIDs))

	// Track any stuck-task error to return after all workers finish
	var stuckErrors []string

	// Initialize all results as not started
	for i, id := range taskIDs {
		results[i] = workerResult{
			TaskID:  id,
			TaskDir: filepath.Join(workDir, id),
			Status:  "not_started",
		}
	}

	// Semaphore for max concurrent workers
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for i, taskID := range taskIDs {
		wg.Add(1)
		idx := i
		tid := taskID

		go func() {
			defer wg.Done()
			sem <- struct{}{}        // acquire slot
			defer func() { <-sem }() // release slot

			// Check context before starting
			select {
			case <-ctx.Done():
				mu.Lock()
				results[idx].Status = "skipped_drain"
				results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
				mu.Unlock()
				return
			default:
			}

			taskDir := filepath.Join(workDir, tid)

			// ── Retry tracking ──────────────────────────────────────────────
			// Read/increment the attempts counter before dispatching.
			attemptsPath := filepath.Join(taskDir, "attempts")
			var attempts int
			if data, err := os.ReadFile(attemptsPath); err == nil {
				fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &attempts)
			}
			attempts++
			os.MkdirAll(taskDir, 0o755)
			os.WriteFile(attemptsPath, []byte(fmt.Sprintf("%d\n", attempts)), 0o644)

			outputPath := filepath.Join(taskDir, "OUTPUT.md")
			if attempts >= 2 && !fileExists(outputPath) {
				// Task has been attempted twice without producing OUTPUT.md → STUCK
				stuckMsg := fmt.Sprintf("STUCK: %s attempted %d times without completing", tid, attempts)
				fmt.Println(stuckMsg)

				// Write to stuck.md in project root
				stuckPath := filepath.Join(projectDir, "stuck.md")
				f, ferr := os.OpenFile(stuckPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
				if ferr == nil {
					ts := time.Now().UTC().Format(time.RFC3339)
					fmt.Fprintf(f, "\n## %s — %s\n\nAttempted %d times without OUTPUT.md.\n",
						tid, ts, attempts)
					f.Close()
				}

				// Remove from queue so it won't be picked up again
				removeFromQueue(projectDir, tid)

				mu.Lock()
				results[idx].Status = "stuck"
				results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
				results[idx].Attempts = attempts
				stuckErrors = append(stuckErrors, stuckMsg)
				mu.Unlock()
				return
			}

			// ── Load BRIEF ────────────────────────────────────────────────
			briefPath := filepath.Join(taskDir, "BRIEF.md")
			briefData, err := os.ReadFile(briefPath)
			if err != nil {
				mu.Lock()
				results[idx].Status = "failed"
				results[idx].ExitCode = 1
				results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
				results[idx].Attempts = attempts
				mu.Unlock()
				return
			}

			prompt := constructorPrompt + "\n\n---\n\n" + string(briefData)

			// ── Launch worker process ─────────────────────────────────────
			// Worker gets its own process group for clean kill.
			// NOTE: no --timeout flag is passed; the 1hr cap is enforced below.
			c := exec.Command("claude", "--dangerously-skip-permissions", "-p", prompt)
			c.Dir = taskDir
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			if err := c.Start(); err != nil {
				mu.Lock()
				results[idx].Status = "failed"
				results[idx].ExitCode = 1
				results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
				results[idx].Attempts = attempts
				mu.Unlock()
				return
			}

			// PGID == PID because Setpgid:true
			pgid := c.Process.Pid

			// killProc sends SIGTERM → waits 3s → SIGKILL, then drains waitCh.
			killProc := func(waitCh <-chan error) {
				syscall.Kill(-pgid, syscall.SIGTERM)
				select {
				case <-waitCh:
					return // exited cleanly on SIGTERM
				case <-time.After(3 * time.Second):
				}
				syscall.Kill(-pgid, syscall.SIGKILL)
				<-waitCh // always drain
			}

			// Collect wait result in a buffered channel
			waitCh := make(chan error, 1)
			go func() { waitCh <- c.Wait() }()

			// ── 1-hour hard cap + overall context ────────────────────────
			workerTimer := time.NewTimer(workerTimeout)
			defer workerTimer.Stop()

			var waitErr error
			var timedOut bool

			select {
			case waitErr = <-waitCh:
				// Process finished naturally — good or bad exit code.

			case <-workerTimer.C:
				// 1-hour per-worker cap hit.
				timedOut = true
				fmt.Printf("Worker for %s exceeded 1hr cap — killing\n", tid)
				killProc(waitCh)

			case <-ctx.Done():
				// Overall context cancelled (global --timeout or signal).
				timedOut = true
				fmt.Printf("Context cancelled — killing worker for %s\n", tid)
				killProc(waitCh)
			}

			// If timed out or cancelled: leave task in queue (don't mark complete).
			if timedOut {
				mu.Lock()
				results[idx].Status = "timed_out"
				results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
				results[idx].Attempts = attempts
				mu.Unlock()
				return
			}

			// Determine exit status
			exitCode := 0
			if waitErr != nil {
				if exitErr, ok := waitErr.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
			}

			outputPresent := fileExists(filepath.Join(taskDir, "OUTPUT.md"))
			handoffPresent := fileExists(filepath.Join(taskDir, "HANDOFF.md"))
			progressEntries := countProgressEntries(filepath.Join(taskDir, "PROGRESS.md"))

			status := "failed"
			if exitCode == 0 && outputPresent {
				status = "completed"
			}

			mu.Lock()
			results[idx].ExitCode = exitCode
			results[idx].ExitedAt = time.Now().UTC().Format(time.RFC3339)
			results[idx].OutputPresent = outputPresent
			results[idx].HandoffPresent = handoffPresent
			results[idx].ProgressEntries = progressEntries
			results[idx].Status = status
			results[idx].Attempts = attempts
			mu.Unlock()
		}()
	}

	wg.Wait()
	endTime := time.Now().UTC()

	// Build summary
	report := fullWorkerReport{
		Status:    "complete",
		StartTime: startTime.Format(time.RFC3339),
		EndTime:   endTime.Format(time.RFC3339),
		QueueFile: queueFile,
		TaskIDs:   taskIDs,
		Workers:   results,
	}

	for _, r := range results {
		report.Summary.Total++
		switch r.Status {
		case "completed":
			report.Summary.Completed++
		case "timed_out":
			report.Summary.TimedOut++
		case "killed":
			report.Summary.Killed++
		case "failed":
			report.Summary.Failed++
		case "not_started":
			report.Summary.NotStarted++
		case "stuck":
			report.Summary.Stuck++
		case "skipped_drain":
			report.Summary.SkippedDrain++
		}
	}

	// Write report
	reportData, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling report: %w", err)
	}
	if err := os.WriteFile(reportPath, reportData, 0o644); err != nil {
		return "", fmt.Errorf("writing report: %w", err)
	}

	// Return stuck error if any task was stuck
	if len(stuckErrors) > 0 {
		return reportPath, fmt.Errorf("%s", strings.Join(stuckErrors, "; "))
	}

	return reportPath, nil
}

// buildConstructorPrompt builds the combined constructor prompt from skill-dir assets.
func buildConstructorPrompt(skillDir, projectDir string) (string, error) {
	if skillDir == "" {
		return "", fmt.Errorf("--skill-dir is required")
	}

	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "cowork"
	}

	s, _ := state.Load(projectDir)
	runCount := 0
	if s != nil {
		runCount = s.RunCount
	}

	substitute := func(content string) string {
		content = strings.ReplaceAll(content, "{{PROJECT_PATH}}", projectDir)
		content = strings.ReplaceAll(content, "{{BINARY_PATH}}", binaryPath)
		content = strings.ReplaceAll(content, "{{RUN_COUNT}}", fmt.Sprintf("%d", runCount))
		return content
	}

	var combined strings.Builder

	// Read cowork-cli.md (optional)
	cliRefPath := filepath.Join(skillDir, "assets", "scripts", "cowork-cli.md")
	if data, err := os.ReadFile(cliRefPath); err == nil {
		combined.WriteString(substitute(string(data)))
		combined.WriteString("\n\n---\n\n")
	}

	// Read CONSTRUCTOR.md (required)
	constructorPath := filepath.Join(skillDir, "assets", "scripts", "CONSTRUCTOR.md")
	data, err := os.ReadFile(constructorPath)
	if err != nil {
		return "", fmt.Errorf("reading CONSTRUCTOR.md: %w", err)
	}
	combined.WriteString(substitute(string(data)))

	return combined.String(), nil
}

// readTaskIDs extracts task IDs from a queue file using the heading regex.
func readTaskIDs(queueFile string) []string {
	data, err := os.ReadFile(queueFile)
	if err != nil {
		return nil
	}

	var ids []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if m := taskHeadingRe.FindStringSubmatch(scanner.Text()); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// countProgressEntries counts lines starting with "[" in a PROGRESS.md file.
func countProgressEntries(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	count := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "[") {
			count++
		}
	}
	return count
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
