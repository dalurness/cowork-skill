# cowork CLI Reference

Binary: `{{BINARY_PATH}}`  
Project: `{{PROJECT_PATH}}`  
Run: `{{RUN_COUNT}}`

You are operating inside a `cowork`-managed project. Use the commands below to
interact with the project — do NOT write to `state.json`, `queue/todo.md`, or
`questions/` / `decisions/` directly. The binary owns those files. Agent-written
files (`plan/`, `OVERVIEW.md`, `work/TASK-XXX/DISCOVERY.md`, `updates/`) are
yours to write directly.

---

## Task Management

### Create a task (returns task ID on stdout)
```bash
cowork task create \
  --title "Short descriptive title" \
  --mode research|spec|implementation \
  --scope "Exactly what this covers" \
  --out-of-scope "What to skip" \
  --context "path/to/file.md:why relevant" \
  --output "What to produce" \
  [--priority high|normal|low] \
  [--blocked-by TASK-003] \
  [--decisions "DECISION-001:what was decided"]
```
Creates `work/TASK-XXX/BRIEF.md`. Does NOT add to queue — call `cowork queue add` separately.
Returns the assigned task ID (`TASK-XXX`) on stdout.

### Record progress (append timestamped entry to PROGRESS.md)
```bash
cowork task progress --task TASK-001 --message "Completed auth module research"
```

### Write handoff (overwrites HANDOFF.md)
```bash
cowork task handoff --task TASK-001 --content "Reached decision point on X..."
```

### Signal completion (copies file to OUTPUT.md)
```bash
cowork task output --task TASK-001 --file ./my-output.md
```
Archiving happens at end of run — not immediately.

---

## Queue Management

### Add task to queue
```bash
cowork queue add --task TASK-001 [--priority high|normal|low] [--top]
```
`--top` inserts at front (use for resumptions).

### Remove task from queue
```bash
cowork queue remove --task TASK-001
```

### List ready tasks
```bash
cowork queue list
```

---

## Questions (blocking issues needing human input)

### Raise a question (blocks the forever loop until answered)
```bash
cowork question create \
  --question "Should we use DID+VC or simple JWT?" \
  --options "A: DID+VC — more secure, slower; B: JWT — faster, well-understood" \
  [--context "Background on why this matters..."] \
  [--recommendation "A — aligns with agent identity work"]
```
Returns `QUESTION-XXX` on stdout.

### List unanswered questions
```bash
cowork question list
```

### Archive answered question (after integrating decision into plan)
```bash
cowork question archive --id QUESTION-001
```

---

## Decisions (answers to questions — submitted by humans via Nova)

### List submitted decisions
```bash
cowork decision list
```

---

## Plan File Management

### Create a feature plan file
```bash
cowork plan create --feature "auth-system"
# Returns path: plan/features/auth-system.md
```

### Snapshot before major rewrite
```bash
cowork plan snapshot --feature "auth-system"
```

### Mark question integrated into plan
```bash
cowork plan integrate --question-id QUESTION-001 --feature "auth-system"
```
Archives the question/decision pair to `history/`.

---

## Logging

### Write a run log entry (also appends to updates/)
```bash
cowork log run --summary "Run 7: TASK-005 complete, TASK-006 timed out"
```

---

## Mark task complete and archive

```bash
cowork done add --task TASK-001 --summary "What was produced (1 paragraph)"
```
Archives worker files from `work/TASK-001/` to `history/`, removes from queue.

---

## Key File Locations (relative to project root)

| File | Owner | Notes |
|------|-------|-------|
| `OVERVIEW.md` | Human / Agent | Vision doc — don't overwrite |
| `state.json` | Binary | Never write directly |
| `queue/todo.md` | Binary | Use `cowork queue` commands |
| `work/TASK-XXX/BRIEF.md` | Binary | Created by `cowork task create` |
| `work/TASK-XXX/PROGRESS.md` | Agent (via CLI) | Workers update continuously |
| `work/TASK-XXX/OUTPUT.md` | Agent (via CLI) | Final output — includes `## Discoveries` section |
| `work/TASK-XXX/HANDOFF.md` | Agent (via CLI) | State if interrupted |
| `plan/OVERVIEW.md` | Agent (direct write) | Synthesized plan |
| `plan/features/*.md` | Agent (direct write) | Per-feature specs |
| `done.md` | Binary | Completed task log |
| `questions/QUESTION-XXX.md` | Binary | Created by `cowork question create` |
| `decisions/QUESTION-XXX.md` | Binary | Created by `cowork decision submit` |
| `updates/YYYY-MM-DD.md` | Agent (direct write) | Human-readable run summaries |
| `log/*.json` | Binary | Structured run logs |
| `history/` | Binary | Archived task files |

---
