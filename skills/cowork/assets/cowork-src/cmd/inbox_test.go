package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ----------------------------------------------------------------
// TestInboxAdd — creates a file in inbox/ with the given message
// ----------------------------------------------------------------

func TestInboxAdd(t *testing.T) {
	dir := t.TempDir()

	exitCode, output := runCowork(t, 10*time.Second,
		"inbox", "add", "test message", "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	// Output should be the path to the created file
	createdPath := strings.TrimSpace(output)
	if createdPath == "" {
		t.Fatal("expected file path in output, got empty string")
	}

	// File should exist
	data, err := os.ReadFile(createdPath)
	if err != nil {
		t.Fatalf("expected file at %s to exist: %v", createdPath, err)
	}

	// Content should match
	if !strings.Contains(string(data), "test message") {
		t.Errorf("expected 'test message' in file, got: %s", string(data))
	}

	// File should be inside inbox/
	if !strings.Contains(createdPath, "inbox") {
		t.Errorf("expected path to contain 'inbox', got: %s", createdPath)
	}
}

// ----------------------------------------------------------------
// TestInboxList — adds 2 items, lists them
// ----------------------------------------------------------------

func TestInboxList(t *testing.T) {
	dir := t.TempDir()

	// Add two items
	_, _ = runCowork(t, 10*time.Second, "inbox", "add", "first message", "--project", dir)
	// Sleep briefly to avoid same-second filenames
	time.Sleep(1100 * time.Millisecond)
	_, _ = runCowork(t, 10*time.Second, "inbox", "add", "second message", "--project", dir)

	exitCode, output := runCowork(t, 10*time.Second, "inbox", "list", "--project", dir)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "first message") {
		t.Errorf("expected 'first message' in list output, got:\n%s", output)
	}
	if !strings.Contains(output, "second message") {
		t.Errorf("expected 'second message' in list output, got:\n%s", output)
	}
}

// ----------------------------------------------------------------
// TestInboxListEmpty — empty inbox returns "Inbox is empty"
// ----------------------------------------------------------------

func TestInboxListEmpty(t *testing.T) {
	dir := t.TempDir()

	exitCode, output := runCowork(t, 10*time.Second, "inbox", "list", "--project", dir)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "Inbox is empty") {
		t.Errorf("expected 'Inbox is empty', got:\n%s", output)
	}
}

// ----------------------------------------------------------------
// TestInboxHandle — item moves to history/, removed from inbox/
// ----------------------------------------------------------------

func TestInboxHandle(t *testing.T) {
	dir := t.TempDir()

	// Add an item
	_, addOut := runCowork(t, 10*time.Second, "inbox", "add", "handle me", "--project", dir)
	createdPath := strings.TrimSpace(addOut)

	// Handle it — pass the path relative to project dir
	relPath := strings.TrimPrefix(createdPath, dir+"/")
	exitCode, output := runCowork(t, 10*time.Second,
		"inbox", "handle", relPath, "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	// Original should no longer exist
	if _, err := os.Stat(createdPath); err == nil {
		t.Errorf("expected inbox file to be gone after handle, but it still exists: %s", createdPath)
	}

	// Something should be in history/
	histEntries, err := os.ReadDir(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatalf("expected history/ to exist: %v", err)
	}
	if len(histEntries) == 0 {
		t.Error("expected at least one file in history/ after handle")
	}

	// At least one history file should have "inbox" in its name
	found := false
	for _, e := range histEntries {
		if strings.Contains(e.Name(), "inbox") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a history file with 'inbox' in name, got: %v", histEntries)
	}
}

// ----------------------------------------------------------------
// TestInboxRespond — creates outbox file, moves original to history/
// ----------------------------------------------------------------

func TestInboxRespond(t *testing.T) {
	dir := t.TempDir()

	// Add an item
	_, addOut := runCowork(t, 10*time.Second, "inbox", "add", "what is the status?", "--project", dir)
	createdPath := strings.TrimSpace(addOut)
	relPath := strings.TrimPrefix(createdPath, dir+"/")

	exitCode, output := runCowork(t, 10*time.Second,
		"inbox", "respond", relPath, "All is well.", "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "Response written to outbox/") {
		t.Errorf("expected confirmation message, got:\n%s", output)
	}

	// Original inbox file should be gone
	if _, err := os.Stat(createdPath); err == nil {
		t.Errorf("expected inbox file to be moved to history/, but it still exists")
	}

	// Outbox should have a file
	outboxEntries, err := os.ReadDir(filepath.Join(dir, "outbox"))
	if err != nil {
		t.Fatalf("expected outbox/ to exist: %v", err)
	}
	if len(outboxEntries) == 0 {
		t.Fatal("expected at least one file in outbox/")
	}

	// Read the outbox file
	outboxFile := filepath.Join(dir, "outbox", outboxEntries[0].Name())
	outboxData, err := os.ReadFile(outboxFile)
	if err != nil {
		t.Fatalf("reading outbox file: %v", err)
	}

	// Should have "Re:" header
	if !strings.Contains(string(outboxData), "# Re:") {
		t.Errorf("expected '# Re:' header in outbox file, got:\n%s", string(outboxData))
	}

	// Should contain the answer
	if !strings.Contains(string(outboxData), "All is well.") {
		t.Errorf("expected answer in outbox file, got:\n%s", string(outboxData))
	}

	// History should have the original inbox file
	histEntries, err := os.ReadDir(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatalf("expected history/ to exist: %v", err)
	}
	found := false
	for _, e := range histEntries {
		if strings.Contains(e.Name(), "inbox") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected history/ to contain the original inbox file")
	}
}

// ----------------------------------------------------------------
// TestOutboxList — lists outbox items
// ----------------------------------------------------------------

func TestOutboxList(t *testing.T) {
	dir := t.TempDir()

	// Manually create an outbox file
	outboxDir := filepath.Join(dir, "outbox")
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatalf("mkdir outbox/: %v", err)
	}
	outboxFile := filepath.Join(outboxDir, "20260101-120000.md")
	if err := os.WriteFile(outboxFile, []byte("# Re: some-inbox-item.md\n\nHere is the answer.\n"), 0o644); err != nil {
		t.Fatalf("write outbox file: %v", err)
	}

	exitCode, output := runCowork(t, 10*time.Second, "outbox", "list", "--project", dir)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "20260101-120000.md") {
		t.Errorf("expected outbox file to appear in list, got:\n%s", output)
	}
}

// ----------------------------------------------------------------
// TestOutboxArchive — moves outbox file to history/
// ----------------------------------------------------------------

func TestOutboxArchive(t *testing.T) {
	dir := t.TempDir()

	// Manually create an outbox file
	outboxDir := filepath.Join(dir, "outbox")
	if err := os.MkdirAll(outboxDir, 0o755); err != nil {
		t.Fatalf("mkdir outbox/: %v", err)
	}
	outboxFile := filepath.Join(outboxDir, "20260101-120000.md")
	if err := os.WriteFile(outboxFile, []byte("# Re: some.md\n\nDone.\n"), 0o644); err != nil {
		t.Fatalf("write outbox file: %v", err)
	}

	relPath := "outbox/20260101-120000.md"
	exitCode, output := runCowork(t, 10*time.Second,
		"outbox", "archive", relPath, "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", exitCode, output)
	}

	if !strings.Contains(output, "Archived:") {
		t.Errorf("expected 'Archived:' in output, got:\n%s", output)
	}

	// Original should be gone
	if _, err := os.Stat(outboxFile); err == nil {
		t.Errorf("expected outbox file to be moved to history/, but it still exists")
	}

	// History should have something
	histEntries, err := os.ReadDir(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatalf("expected history/ to exist: %v", err)
	}
	found := false
	for _, e := range histEntries {
		if strings.Contains(e.Name(), "outbox") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a history file with 'outbox' in name, got: %v", histEntries)
	}
}

// ----------------------------------------------------------------
// TestInboxRoundtrip — full flow
// ----------------------------------------------------------------

func TestInboxRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// 1. inbox add
	exitCode, addOut := runCowork(t, 10*time.Second,
		"inbox", "add", "roundtrip question", "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("inbox add failed: %s", addOut)
	}
	inboxPath := strings.TrimSpace(addOut)
	relInboxPath := strings.TrimPrefix(inboxPath, dir+"/")

	// 2. inbox respond
	exitCode, respondOut := runCowork(t, 10*time.Second,
		"inbox", "respond", relInboxPath, "roundtrip answer", "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("inbox respond failed: %s", respondOut)
	}

	// 3. outbox list — should show the response
	exitCode, listOut := runCowork(t, 10*time.Second, "outbox", "list", "--project", dir)
	if exitCode != 0 {
		t.Fatalf("outbox list failed: %s", listOut)
	}
	if strings.Contains(listOut, "Outbox is empty") {
		t.Fatalf("expected outbox to have items, got 'Outbox is empty'")
	}

	// Parse the outbox file path from list output: "outbox/XXXX.md — ..."
	var outboxRelPath string
	for _, line := range strings.Split(strings.TrimSpace(listOut), "\n") {
		parts := strings.SplitN(line, " — ", 2)
		if len(parts) >= 1 && strings.HasPrefix(parts[0], "outbox/") {
			outboxRelPath = strings.TrimSpace(parts[0])
			break
		}
	}
	if outboxRelPath == "" {
		t.Fatalf("could not parse outbox path from list output: %s", listOut)
	}

	// 4. outbox archive
	exitCode, archOut := runCowork(t, 10*time.Second,
		"outbox", "archive", outboxRelPath, "--project", dir,
	)
	if exitCode != 0 {
		t.Fatalf("outbox archive failed: %s", archOut)
	}

	// 5. Verify history/ has both inbox and outbox files
	histEntries, err := os.ReadDir(filepath.Join(dir, "history"))
	if err != nil {
		t.Fatalf("reading history/: %v", err)
	}

	var hasInbox, hasOutbox bool
	for _, e := range histEntries {
		if strings.Contains(e.Name(), "inbox") {
			hasInbox = true
		}
		if strings.Contains(e.Name(), "outbox") {
			hasOutbox = true
		}
	}

	if !hasInbox {
		t.Error("expected history/ to contain an inbox file")
	}
	if !hasOutbox {
		t.Error("expected history/ to contain an outbox file")
	}
}
