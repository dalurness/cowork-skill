package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// --- Report types ---

type PendingReport struct {
	Status    string   `json:"status"`
	StartTime string   `json:"startTime"`
	QueueFile string   `json:"queueFile"`
	TaskIDs   []string `json:"taskIds"`
}

type WorkerResult struct {
	TaskID           string `json:"taskId"`
	TaskDir          string `json:"taskDir"`
	Status           string `json:"status"` // completed, timed_out, killed, failed, not_started, skipped_drain
	ExitedAt         string `json:"exitedAt,omitempty"`
	ExitCode         *int   `json:"exitCode,omitempty"`
	OutputPresent    bool   `json:"outputPresent"`
	HandoffPresent   bool   `json:"handoffPresent"`
	DiscoveryPresent bool   `json:"discoveryPresent"`
	ProgressEntries  int    `json:"progressEntries"`
}

type Summary struct {
	Total        int `json:"total"`
	Completed    int `json:"completed"`
	TimedOut     int `json:"timedOut"`
	Killed       int `json:"killed"`
	Failed       int `json:"failed"`
	NotStarted   int `json:"notStarted"`
	SkippedDrain int `json:"skippedDrain"`
}

type CompleteReport struct {
	Status    string         `json:"status"`
	StartTime string         `json:"startTime"`
	EndTime   string         `json:"endTime"`
	QueueFile string         `json:"queueFile"`
	TaskIDs   []string       `json:"taskIds"`
	Workers   []WorkerResult `json:"workers"`
	Summary   Summary        `json:"summary"`
}

// --- Helpers ---

func writeJSON(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func countProgressEntries(taskDir string) int {
	path := filepath.Join(taskDir, "PROGRESS.md")
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	re := regexp.MustCompile(`\[\d{2}:\d{2}\]`)
	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if re.MatchString(scanner.Text()) {
			count++
		}
	}
	return count
}

// parseQueue reads a todo.md file and extracts TASK-XXX IDs in order.
func parseQueue(queuePath string) ([]string, error) {
	f, err := os.Open(queuePath)
	if err != nil {
		return nil, fmt.Errorf("cannot open queue file: %w", err)
	}
	defer f.Close()

	re := regexp.MustCompile(`(TASK-\d+)`)
	var taskIDs []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		matches := re.FindStringSubmatch(scanner.Text())
		if len(matches) > 0 {
			id := matches[1]
			if !seen[id] {
				taskIDs = append(taskIDs, id)
				seen[id] = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading queue file: %w", err)
	}
	return taskIDs, nil
}

// moveFile moves a file, falling back to copy+delete for cross-device moves.
func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}
	// Cross-device fallback: copy + delete
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	in.Close()
	return os.Remove(src)
}

// archiveToHistory moves worker-written files to history/ with timestamped names.
// BRIEF.md is left in place.
func archiveToHistory(taskDir, taskID, historyDir string) {
	timestamp := time.Now().UTC().Format("20060102T150405")
	filesToMove := []string{"OUTPUT.md", "PROGRESS.md", "HANDOFF.md", "DISCOVERY.md"}
	for _, name := range filesToMove {
		src := filepath.Join(taskDir, name)
		if fileExists(src) {
			dst := filepath.Join(historyDir, fmt.Sprintf("%s-%s-%s", timestamp, taskID, name))
			if err := moveFile(src, dst); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to archive %s: %v\n", src, err)
			}
		}
	}
}

// --- Worker types ---

type runningWorker struct {
	TaskID string
	TaskDir string
	Cmd    *exec.Cmd
}

// launchWorker starts a worker process. Returns nil cmd if the worker could not be launched.
// On success, a goroutine waits for the process and sends the result on completions.
func launchWorker(taskID, taskDir, command, constructorPath string, completions chan<- WorkerResult) *exec.Cmd {
	result := WorkerResult{
		TaskID:  taskID,
		TaskDir: taskDir,
	}

	briefPath := filepath.Join(taskDir, "BRIEF.md")
	if !fileExists(briefPath) {
		result.Status = "not_started"
		result.ExitedAt = time.Now().UTC().Format(time.RFC3339)
		completions <- result
		return nil
	}

	constructorContent, err := os.ReadFile(constructorPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker %s: cannot read constructor: %v\n", taskID, err)
		result.Status = "not_started"
		result.ExitedAt = time.Now().UTC().Format(time.RFC3339)
		completions <- result
		return nil
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
		result.Status = "not_started"
		result.ExitedAt = time.Now().UTC().Format(time.RFC3339)
		completions <- result
		return nil
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = taskDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader(string(constructorContent))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "worker %s: failed to start: %v\n", taskID, err)
		result.Status = "not_started"
		result.ExitedAt = time.Now().UTC().Format(time.RFC3339)
		completions <- result
		return nil
	}

	go func() {
		err := cmd.Wait()
		r := WorkerResult{
			TaskID:  taskID,
			TaskDir: taskDir,
		}
		r.ExitedAt = time.Now().UTC().Format(time.RFC3339)
		if err == nil {
			exitCode := 0
			r.ExitCode = &exitCode
			r.Status = "completed"
		} else {
			if exitErr, ok := err.(*exec.ExitError); ok {
				code := exitErr.ExitCode()
				r.ExitCode = &code
			}
			r.Status = "failed"
		}
		r.OutputPresent = fileExists(filepath.Join(taskDir, "OUTPUT.md"))
		r.HandoffPresent = fileExists(filepath.Join(taskDir, "HANDOFF.md"))
		r.DiscoveryPresent = fileExists(filepath.Join(taskDir, "DISCOVERY.md"))
		r.ProgressEntries = countProgressEntries(taskDir)
		completions <- r
	}()

	return cmd
}

func inDrainWindow(startTime time.Time, timeout, drainWindow time.Duration) bool {
	elapsed := time.Since(startTime)
	remaining := timeout - elapsed
	return remaining < drainWindow
}

func main() {
	workDir := flag.String("work-dir", "", "Path to work/ directory (required)")
	queuePath := flag.String("queue", "", "Path to queue/todo.md (required)")
	maxWorkers := flag.Int("max-workers", 3, "Max concurrent workers")
	drainWindowStr := flag.String("drain-window", "10m", "Stop launching new workers this long before timeout")
	timeoutStr := flag.String("timeout", "15m", "Total run duration before workers are signaled to stop")
	killAfterStr := flag.String("kill-after", "18m", "Time before workers are force-killed")
	reportPath := flag.String("report", "", "Path to write the JSON run report (required)")
	command := flag.String("command", "", "Command used to start each worker (required)")
	constructor := flag.String("constructor", "", "Path to CONSTRUCTOR.md (required)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: worker-manager [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	// Validate required flags
	if *workDir == "" {
		fmt.Fprintf(os.Stderr, "error: --work-dir is required\n")
		os.Exit(3)
	}
	if *queuePath == "" {
		fmt.Fprintf(os.Stderr, "error: --queue is required\n")
		os.Exit(3)
	}
	if *reportPath == "" {
		fmt.Fprintf(os.Stderr, "error: --report is required\n")
		os.Exit(3)
	}
	if *constructor == "" {
		fmt.Fprintf(os.Stderr, "error: --constructor is required\n")
		os.Exit(3)
	}
	if *command == "" {
		fmt.Fprintf(os.Stderr, "error: --command is required\n")
		os.Exit(3)
	}

	timeout, err := time.ParseDuration(*timeoutStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --timeout: %v\n", err)
		os.Exit(3)
	}
	killAfter, err := time.ParseDuration(*killAfterStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --kill-after: %v\n", err)
		os.Exit(3)
	}
	drainWindow, err := time.ParseDuration(*drainWindowStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid --drain-window: %v\n", err)
		os.Exit(3)
	}

	// Parse queue
	taskIDs, err := parseQueue(*queuePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(3)
	}

	startTime := time.Now().UTC()

	// Determine history directory (project root is parent of work/)
	projectRoot := filepath.Dir(filepath.Clean(*workDir))
	historyDir := filepath.Join(projectRoot, "history")

	// Zero tasks: write empty complete report and exit
	if len(taskIDs) == 0 {
		report := CompleteReport{
			Status:    "complete",
			StartTime: startTime.Format(time.RFC3339),
			EndTime:   time.Now().UTC().Format(time.RFC3339),
			QueueFile: *queuePath,
			TaskIDs:   []string{},
			Workers:   []WorkerResult{},
			Summary:   Summary{},
		}
		if err := writeJSON(*reportPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}

	// Write pending report
	pending := PendingReport{
		Status:    "pending",
		StartTime: startTime.Format(time.RFC3339),
		QueueFile: *queuePath,
		TaskIDs:   taskIDs,
	}
	if err := writeJSON(*reportPath, pending); err != nil {
		fmt.Fprintf(os.Stderr, "error writing pending report: %v\n", err)
		os.Exit(3)
	}

	// --- Dynamic work loop ---
	completions := make(chan WorkerResult, *maxWorkers)
	inFlight := make(map[string]*exec.Cmd) // taskID → cmd
	var results []WorkerResult
	queueIdx := 0

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	timedOut := false

	for {
		// Fill worker slots (only if not in drain window and not timed out)
		for len(inFlight) < *maxWorkers && queueIdx < len(taskIDs) && !timedOut && !inDrainWindow(startTime, timeout, drainWindow) {
			taskID := taskIDs[queueIdx]
			taskDir := filepath.Join(*workDir, taskID)
			queueIdx++

			cmd := launchWorker(taskID, taskDir, *command, *constructor, completions)
			if cmd != nil {
				inFlight[taskID] = cmd
				fmt.Fprintf(os.Stderr, "launched worker: %s (%d/%d in-flight)\n", taskID, len(inFlight), *maxWorkers)
			}
			// If cmd is nil, launchWorker already sent a result on completions.
			// We'll pick it up in the select below.
		}

		// Nothing running and nothing more to launch → done
		if len(inFlight) == 0 {
			// Drain any results from failed-to-launch workers
			drainLoop:
			for {
				select {
				case r := <-completions:
					results = append(results, r)
				default:
					break drainLoop
				}
			}
			break
		}

		// Wait for a worker to finish or timeout
		select {
		case r := <-completions:
			if _, ok := inFlight[r.TaskID]; ok {
				delete(inFlight, r.TaskID)
				// Archive to history on clean completion
				if r.Status == "completed" && r.OutputPresent {
					archiveToHistory(r.TaskDir, r.TaskID, historyDir)
				}
				fmt.Fprintf(os.Stderr, "worker finished: %s [%s] (%d in-flight)\n", r.TaskID, r.Status, len(inFlight))
			}
			results = append(results, r)

		case <-timeoutTimer.C:
			timedOut = true
			fmt.Fprintf(os.Stderr, "timeout reached, sending SIGTERM to %d workers\n", len(inFlight))
			for _, cmd := range inFlight {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			}

			// Wait for graceful shutdown or force kill
			killDeadline := time.After(killAfter - timeout)
			for len(inFlight) > 0 {
				select {
				case r := <-completions:
					if _, ok := inFlight[r.TaskID]; ok {
						delete(inFlight, r.TaskID)
						r.Status = "timed_out"
						fmt.Fprintf(os.Stderr, "worker stopped: %s [timed_out] (%d in-flight)\n", r.TaskID, len(inFlight))
					}
					results = append(results, r)

				case <-killDeadline:
					fmt.Fprintf(os.Stderr, "kill-after reached, sending SIGKILL to %d workers\n", len(inFlight))
					for _, cmd := range inFlight {
						syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
					}
					// Collect remaining
					for len(inFlight) > 0 {
						r := <-completions
						if _, ok := inFlight[r.TaskID]; ok {
							delete(inFlight, r.TaskID)
							r.Status = "killed"
							fmt.Fprintf(os.Stderr, "worker killed: %s (%d in-flight)\n", r.TaskID, len(inFlight))
						}
						results = append(results, r)
					}
				}
			}
		}

		// After timeout handling, break out of main loop
		if timedOut {
			break
		}
	}

	// Mark remaining queue items as skipped_drain
	for i := queueIdx; i < len(taskIDs); i++ {
		results = append(results, WorkerResult{
			TaskID:  taskIDs[i],
			TaskDir: filepath.Join(*workDir, taskIDs[i]),
			Status:  "skipped_drain",
		})
	}

	// Build summary
	summary := Summary{Total: len(taskIDs)}
	for _, r := range results {
		switch r.Status {
		case "completed":
			summary.Completed++
		case "timed_out":
			summary.TimedOut++
		case "killed":
			summary.Killed++
		case "failed":
			summary.Failed++
		case "not_started":
			summary.NotStarted++
		case "skipped_drain":
			summary.SkippedDrain++
		}
	}

	// Write complete report
	report := CompleteReport{
		Status:    "complete",
		StartTime: startTime.Format(time.RFC3339),
		EndTime:   time.Now().UTC().Format(time.RFC3339),
		QueueFile: *queuePath,
		TaskIDs:   taskIDs,
		Workers:   results,
		Summary:   summary,
	}
	if err := writeJSON(*reportPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		os.Exit(3)
	}

	// Exit code
	if summary.Completed == summary.Total {
		os.Exit(0)
	}
	if summary.Completed > 0 {
		os.Exit(1)
	}
	os.Exit(2)
}
