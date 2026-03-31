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
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Start the system",
	RunE:  runMain,
}

func init() {
	runCmd.Flags().String("project", ".", "Project root directory")
	runCmd.Flags().String("timeout", "", "Overall run timeout (e.g. 2h). If not set, no timeout.")
	runCmd.Flags().Int("workers", 3, "Max concurrent workers")
	runCmd.Flags().Bool("forever", false, "Run in forever loop mode until project complete")
	runCmd.Flags().Int("pm-every", 5, "Run PM pass every N runs")
	runCmd.Flags().Int("architect-every", 10, "Run architect pass every N runs")
	runCmd.Flags().String("skill-dir", "", "Path to skill directory (for scripts/)")
	runCmd.Flags().Duration("drain-window", 15*time.Minute, "Stop launching new cycles this long before the overall timeout deadline")
	runCmd.Flags().Duration("poll-interval", 10*time.Minute, "Polling interval for forever mode")
	runCmd.Flags().String("only", "", "Bypass scheduling and run one step directly: orchestrator|pm|architect|worker")
	runCmd.Flags().String("task", "", "Task ID for --only worker (e.g. TASK-004)")
	runCmd.Flags().String("worker-cmd", "", "Mock worker command for testing (empty = use real claude)")
}

func runMain(cmd *cobra.Command, args []string) error {
	projectDir, _ := cmd.Flags().GetString("project")
	forever, _ := cmd.Flags().GetBool("forever")
	only, _ := cmd.Flags().GetString("only")
	timeoutStr, _ := cmd.Flags().GetString("timeout")

	// Resolve project dir to absolute
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return fmt.Errorf("resolving project path: %w", err)
	}
	projectDir = absProject

	// Build context (with or without overall timeout)
	ctx := context.Background()
	cancel := context.CancelFunc(func() {}) // no-op default
	if timeoutStr != "" {
		d, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return fmt.Errorf("invalid --timeout duration %q: %w", timeoutStr, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), d)
		fmt.Printf("Overall timeout: %v (deadline: %s)\n", d, time.Now().Add(d).Format("15:04:05"))
	}
	defer cancel()

	// --only bypasses all scheduling logic and fires one step directly
	if only != "" {
		return runOnly(ctx, cmd, projectDir, only)
	}

	if forever {
		return runForever(ctx, cmd, projectDir)
	}
	return runOnce(ctx, cmd, projectDir)
}

// runOnly fires a single named step directly, bypassing queue checks and scheduling.
func runOnly(ctx context.Context, cmd *cobra.Command, projectDir, step string) error {
	switch step {
	case "orchestrator":
		fmt.Println("Running orchestrator (forced)...")
		if err := runOrchestrator(ctx, cmd, projectDir); err != nil {
			return err
		}
		archived := archiveCompletedTasks(projectDir)
		if archived > 0 {
			fmt.Printf("Archived %d completed task(s)\n", archived)
		}
		return nil

	case "pm":
		fmt.Println("Running PM pass (forced)...")
		return runScriptSession(ctx, cmd, projectDir, "pm-pass.md")

	case "architect":
		fmt.Println("Running Architect pass (forced)...")
		return runScriptSession(ctx, cmd, projectDir, "architect-pass.md")

	case "worker":
		taskID, _ := cmd.Flags().GetString("task")
		if taskID == "" {
			return fmt.Errorf("--only worker requires --task TASK-XXX")
		}
		return runSingleWorker(ctx, cmd, projectDir, taskID)

	default:
		return fmt.Errorf("unknown --only value %q (valid: orchestrator, pm, architect, worker)", step)
	}
}

// runOnce performs one full cycle:
//  1. Check for blocking questions
//  2. Run orchestrator (reviews completed OUTPUT.mds and/or generates new tasks)
//  3. If tasks exist → run PM/Architect if scheduled, then launch workers
//  4. Exit
func runOnce(ctx context.Context, cmd *cobra.Command, projectDir string) error {
	// 1. Check for unanswered questions
	unanswered := getUnansweredQuestions(projectDir)
	if len(unanswered) > 0 {
		fmt.Println("Blocked: unanswered questions exist:")
		for _, q := range unanswered {
			fmt.Printf("  %s\n", q)
		}
		return fmt.Errorf("resolve questions before running")
	}

	// 2. Always run orchestrator first — it reviews pending OUTPUT.md files
	//    and/or generates new tasks from scratch.
	fmt.Println("Running orchestrator...")
	if err := runOrchestrator(ctx, cmd, projectDir); err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "Orchestrator error: %v\n", err)
	}
	archived := archiveCompletedTasks(projectDir)
	if archived > 0 {
		fmt.Printf("Archived %d completed task(s)\n", archived)
	}

	// 3. Re-read queue after orchestrator pass
	tasks := getReadyTasks(projectDir)
	if len(tasks) == 0 {
		fmt.Println("No ready tasks after orchestrator — nothing to dispatch")
		return nil
	}

	// 4. Increment run counter, check PM/Architect scheduling
	s, err := state.Load(projectDir)
	if err != nil {
		return err
	}
	s.RunCount++
	s.LastRun = time.Now().UTC().Format(time.RFC3339)
	s.Phase = "working"
	if err := state.Save(projectDir, s); err != nil {
		return err
	}

	pmEvery, _ := cmd.Flags().GetInt("pm-every")
	architectEvery, _ := cmd.Flags().GetInt("architect-every")

	hasDoneEntries := fileHasContent(filepath.Join(projectDir, "done.md"))

	// PM pass
	if pmEvery > 0 && s.RunCount%pmEvery == 0 && hasDoneEntries {
		fmt.Printf("Run %d: PM pass scheduled\n", s.RunCount)
		if err := runScriptSession(ctx, cmd, projectDir, "pm-pass.md"); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(os.Stderr, "PM pass error: %v\n", err)
		}
	}

	// Architect pass
	if architectEvery > 0 && s.RunCount%architectEvery == 0 && hasDoneEntries {
		fmt.Printf("Run %d: Architect pass scheduled\n", s.RunCount)
		if err := runScriptSession(ctx, cmd, projectDir, "architect-pass.md"); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			fmt.Fprintf(os.Stderr, "Architect pass error: %v\n", err)
		}
	}

	// 5. Fire worker-manager (each worker is capped at 1hr internally)
	fmt.Printf("Run %d: Starting workers for %d tasks\n", s.RunCount, len(tasks))
	reportPath, err := runWorkers(cmd, projectDir, ctx)
	if err != nil {
		// If stuck task detected, propagate as non-zero exit
		return fmt.Errorf("workers: %w", err)
	}

	// 6. Remove completed tasks from queue; files stay in work/ for orchestrator review
	completed := markCompletionsFromReport(projectDir, reportPath)

	summary := fmt.Sprintf("Run %d: %d tasks processed, %d completed (pending orchestrator review)", s.RunCount, len(tasks), completed)
	writeRunLog(projectDir, summary)

	s.Phase = "idle"
	state.Save(projectDir, s)

	fmt.Println(summary)
	return nil
}

// runForever loops until the project is complete, context is cancelled, or deadline approaches.
func runForever(ctx context.Context, cmd *cobra.Command, projectDir string) error {
	pollInterval, _ := cmd.Flags().GetDuration("poll-interval")

	for {
		// Check context before each iteration
		select {
		case <-ctx.Done():
			fmt.Println("Context cancelled/deadline reached, stopping.")
			return nil
		default:
		}

		// Stop if deadline is too close to fit another full cycle
		drainWindow, _ := cmd.Flags().GetDuration("drain-window")
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			if remaining < drainWindow {
				fmt.Printf("Stopping: only %v until deadline — not enough time for another cycle\n",
					remaining.Round(time.Second))
				return nil
			}
		}

		// Check for blocking questions
		unanswered := getUnansweredQuestions(projectDir)
		if len(unanswered) > 0 {
			fmt.Println("Waiting for answers to:")
			for _, q := range unanswered {
				fmt.Printf("  %s\n", q)
			}

			s, _ := state.Load(projectDir)
			s.Phase = "blocked"
			state.Save(projectDir, s)

			fmt.Printf("Sleeping %v...\n", pollInterval)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(pollInterval):
			}
			continue
		}

		// Run one full cycle (orchestrator + workers)
		if err := runOnce(ctx, cmd, projectDir); err != nil {
			if ctx.Err() != nil {
				fmt.Println("Context cancelled during run, stopping.")
				return nil
			}
			fmt.Fprintf(os.Stderr, "Run error: %v\n", err)
			// If it's a STUCK error, propagate it
			if strings.Contains(err.Error(), "STUCK:") {
				return err
			}
		}

		// After a cycle, check if the project is now complete:
		// orchestrator just ran and produced no new tasks and no new questions.
		newTasks := getReadyTasks(projectDir)
		newQuestions := getUnansweredQuestions(projectDir)
		if len(newTasks) == 0 && len(newQuestions) == 0 {
			s, _ := state.Load(projectDir)
			s.Phase = "complete"
			state.Save(projectDir, s)
			writeRunLog(projectDir, "Project complete — no remaining tasks or questions")

			fmt.Println("========================================")
			fmt.Println("  PROJECT COMPLETE")
			fmt.Printf("  %d runs finished\n", s.RunCount)
			fmt.Println("========================================")

			cancelRegisteredCrons(s)
			return nil
		}

		// Brief pause before next iteration to avoid spinning on errors
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(2 * time.Second):
		}
	}
}

// getUnansweredQuestions returns question IDs that have no matching decision file.
func getUnansweredQuestions(projectDir string) []string {
	questionsDir := filepath.Join(projectDir, "questions")
	entries, err := os.ReadDir(questionsDir)
	if err != nil {
		return nil
	}

	decisionsDir := filepath.Join(projectDir, "decisions")
	var unanswered []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		decisionPath := filepath.Join(decisionsDir, e.Name())
		if _, err := os.Stat(decisionPath); os.IsNotExist(err) {
			qID := strings.TrimSuffix(e.Name(), ".md")
			// Read question text
			data, _ := os.ReadFile(filepath.Join(questionsDir, e.Name()))
			text := extractSection(string(data), "## Question")
			if text != "" {
				unanswered = append(unanswered, fmt.Sprintf("%s: %s", qID, strings.TrimSpace(text)))
			} else {
				unanswered = append(unanswered, qID)
			}
		}
	}
	return unanswered
}

// getReadyTasks returns task IDs from queue/todo.md.
// Matches the heading format written by `cowork queue add`: ### [TASK-XXX] ...
var taskHeadingRe = regexp.MustCompile(`^#{1,3}\s+\[(TASK-\d+)\]`)

func getReadyTasks(projectDir string) []string {
	p := filepath.Join(projectDir, "queue", "todo.md")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}

	var tasks []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if m := taskHeadingRe.FindStringSubmatch(line); m != nil {
			tasks = append(tasks, m[1])
		}
	}
	return tasks
}

// runOrchestrator injects the orchestrator script into a claude session.
func runOrchestrator(ctx context.Context, cmd *cobra.Command, projectDir string) error {
	return runScriptSession(ctx, cmd, projectDir, "orchestrator.md")
}

// runScriptSession reads a script from the skill scripts/ dir and pipes it to claude.
// The claude process is bound to ctx — if ctx is cancelled, the process is killed.
func runScriptSession(ctx context.Context, cmd *cobra.Command, projectDir, scriptName string) error {
	skillDir, _ := cmd.Flags().GetString("skill-dir")
	if skillDir == "" {
		return fmt.Errorf("--skill-dir is required for script injection")
	}

	scriptPath := filepath.Join(skillDir, "assets", "scripts", scriptName)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// fallback: scripts/ directly under skill-dir (legacy layout)
		scriptPath = filepath.Join(skillDir, "scripts", scriptName)
	}
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("reading script %s: %w", scriptName, err)
	}

	// Resolve binary path
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "cowork"
	}

	// Template helper: substitute runtime values in a string
	substitute := func(content string) string {
		s2, _ := state.Load(projectDir)
		runCount := 0
		if s2 != nil {
			runCount = s2.RunCount
		}
		content = strings.ReplaceAll(content, "{{PROJECT_PATH}}", projectDir)
		content = strings.ReplaceAll(content, "{{BINARY_PATH}}", binaryPath)
		content = strings.ReplaceAll(content, "{{RUN_COUNT}}", fmt.Sprintf("%d", runCount))
		return content
	}

	// Load shared CLI reference (prepended to every script if present)
	var cliRef string
	cliRefPath := filepath.Join(skillDir, "assets", "scripts", "cowork-cli.md")
	if _, err := os.Stat(cliRefPath); os.IsNotExist(err) {
		cliRefPath = filepath.Join(skillDir, "scripts", "cowork-cli.md")
	}
	if refData, err := os.ReadFile(cliRefPath); err == nil {
		cliRef = substitute(string(refData)) + "\n\n---\n\n"
	}

	// Template substitution on the role script
	script := substitute(string(scriptData))

	// Build the prompt: CLI reference preamble + role script
	prompt := cliRef + script

	// Run claude session — bound to ctx for clean cancellation
	c := exec.CommandContext(ctx, "claude", "--dangerously-skip-permissions", "-p", prompt)
	c.Dir = projectDir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	return c.Run()
}

// runSingleWorker fires one worker for a specific task, bypassing the normal queue.
func runSingleWorker(ctx context.Context, cmd *cobra.Command, projectDir, taskID string) error {
	// Verify the task dir and BRIEF exist
	briefPath := filepath.Join(projectDir, "work", taskID, "BRIEF.md")
	if _, err := os.Stat(briefPath); err != nil {
		return fmt.Errorf("task %s not found — expected BRIEF.md at %s", taskID, briefPath)
	}

	// Write a temp queue file with just this task
	tmpQueue, err := os.CreateTemp("", "cowork-single-queue-*.md")
	if err != nil {
		return fmt.Errorf("creating temp queue: %w", err)
	}
	defer os.Remove(tmpQueue.Name())
	fmt.Fprintf(tmpQueue, "### [%s] (normal)\n", taskID)
	tmpQueue.Close()

	// Temporarily override queue and workers
	origQueue, _ := cmd.Flags().GetString("queue")
	origWorkers, _ := cmd.Flags().GetInt("workers")
	cmd.Flags().Set("queue", tmpQueue.Name())
	cmd.Flags().Set("workers", "1")
	defer func() {
		cmd.Flags().Set("queue", origQueue)
		cmd.Flags().Set("workers", fmt.Sprintf("%d", origWorkers))
	}()

	fmt.Printf("Running single worker for %s...\n", taskID)
	reportPath, err := runWorkers(cmd, projectDir, ctx)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}

	// Print result from report
	if data, err := os.ReadFile(reportPath); err == nil {
		var report fullWorkerReport
		if json.Unmarshal(data, &report) == nil && len(report.Workers) > 0 {
			w := report.Workers[0]
			fmt.Printf("%s: %s\n", w.TaskID, w.Status)
		}
	}
	return nil
}

// archiveCompletedTasks scans work/ for task dirs that have OUTPUT.md and archives them.
func archiveCompletedTasks(projectDir string) int {
	workDir := filepath.Join(projectDir, "work")
	entries, err := os.ReadDir(workDir)
	if err != nil {
		return 0
	}

	now := time.Now().UTC()
	archived := 0

	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "TASK-") {
			continue
		}
		taskID := e.Name()
		outputPath := filepath.Join(workDir, taskID, "OUTPUT.md")
		if _, err := os.Stat(outputPath); err != nil {
			continue // No OUTPUT.md — not complete, skip
		}
		if err := archiveTaskFiles(projectDir, taskID, now); err != nil {
			fmt.Fprintf(os.Stderr, "Error archiving %s: %v\n", taskID, err)
			continue
		}
		archived++
	}

	return archived
}

// markCompletionsFromReport reads the worker run report and removes completed
// tasks from the queue.
func markCompletionsFromReport(projectDir, reportPath string) int {
	if reportPath == "" {
		return 0
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		return 0
	}

	var report fullWorkerReport
	if err := json.Unmarshal(data, &report); err != nil {
		return 0
	}

	removed := 0
	for _, w := range report.Workers {
		if w.Status != "completed" {
			continue
		}
		removeFromQueue(projectDir, w.TaskID)
		removed++
	}

	return removed
}

// writeRunLog writes a JSON log entry and appends to the daily update.
func writeRunLog(projectDir, summary string) {
	logDir := filepath.Join(projectDir, "log")
	os.MkdirAll(logDir, 0o755)

	now := time.Now().UTC()
	ts := now.Format("2006-01-02T150405")

	runNum := 0
	s, err := state.Load(projectDir)
	if err == nil {
		runNum = s.RunCount
	}

	logEntry := fmt.Sprintf("{\n  \"timestamp\": %q,\n  \"run\": %d,\n  \"summary\": %q\n}\n",
		now.Format(time.RFC3339), runNum, summary)

	logFile := filepath.Join(logDir, fmt.Sprintf("%s-run-%03d.json", ts, runNum))
	os.WriteFile(logFile, []byte(logEntry), 0o644)

	// Append to updates
	updatesDir := filepath.Join(projectDir, "updates")
	os.MkdirAll(updatesDir, 0o755)
	updateFile := filepath.Join(updatesDir, now.Format("2006-01-02")+".md")
	f, err := os.OpenFile(updateFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		fmt.Fprintf(f, "\n## %s\n\n%s\n", now.Format("15:04"), summary)
		f.Close()
	}
}

func fileHasContent(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// cancelRegisteredCrons cancels all cron IDs stored in state via `openclaw cron delete`.
func cancelRegisteredCrons(s *state.State) {
	if len(s.CronIDs) == 0 {
		return
	}
	fmt.Printf("Cancelling %d registered cron(s)...\n", len(s.CronIDs))
	for _, id := range s.CronIDs {
		out, err := exec.Command("openclaw", "cron", "delete", id).CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Failed to cancel cron %s: %v (%s)\n", id, err, strings.TrimSpace(string(out)))
		} else {
			fmt.Printf("  Cancelled cron %s\n", id)
		}
	}
}
