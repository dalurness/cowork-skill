package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type State struct {
	RunCount        int      `json:"runCount"`
	TaskCounter     int      `json:"taskCounter"`
	QuestionCounter int      `json:"questionCounter"`
	LastRun         string   `json:"lastRun"`
	Phase           string   `json:"phase"`
	CronIDs         []string `json:"cronIds,omitempty"`
}

func DefaultState() *State {
	return &State{
		RunCount:        0,
		TaskCounter:     0,
		QuestionCounter: 0,
		LastRun:         "",
		Phase:           "idle",
	}
}

func StatePath(projectDir string) string {
	return filepath.Join(projectDir, "state.json")
}

func Load(projectDir string) (*State, error) {
	p := StatePath(projectDir)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultState(), nil
		}
		return nil, fmt.Errorf("reading state.json: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing state.json: %w", err)
	}
	return &s, nil
}

func Save(projectDir string, s *State) error {
	p := StatePath(projectDir)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state.json: %w", err)
	}
	data = append(data, '\n')

	// Atomic write: write to temp file, then rename
	dir := filepath.Dir(p)
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

func FormatTaskID(n int) string {
	return fmt.Sprintf("TASK-%03d", n)
}

func FormatQuestionID(n int) string {
	return fmt.Sprintf("QUESTION-%03d", n)
}
