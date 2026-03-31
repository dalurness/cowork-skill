---
name: cowork
description: "Orchestrate long-running AI projects asynchronously using the cowork binary. Workers execute tasks in parallel, an orchestrator reviews output and queues next steps, and PM/Architect passes run on a schedule. Use when: setting up a new async project, initializing cowork for the first time, running or monitoring an active project, manually triggering orchestrator/PM/architect passes, or answering questions that have blocked a project."
---

# Async Project Manager

A deterministic backbone for AI-driven projects. The `cowork` binary manages all file organization — workers and orchestrators interact with the project through CLI commands rather than writing to project files directly.

> **Rule:** Never read from or write to the project directory directly. Always interface through the `cowork` CLI. The binary owns the file layout; direct edits will conflict with or confuse the orchestrator.

## First-time Setup

Build from source. Go 1.20+ and an agent CLI are required (`claude`, `codex`, `opencode`, etc.).
See `cmd/workers.go` to set the command for your tooling — it defaults to `claude`.

```bash
cd <skill-dir>
go build -o ~/bin/cowork .
```

Verify: `cowork --help`

## Starting a New Project

**1. Prepare an overview directory** — create a folder with `OVERVIEW.md` describing the project vision. Add any supporting reference files. `cowork init` will copy everything in.

**2. Initialize:**
```bash
cowork init --project ./my-project --from ./my-notes
# or, if OVERVIEW.md is already in the project dir:
cowork init --project ./my-project
```

**3. Schedule cron jobs, then register their IDs with the project:**
```bash
ID1=$(openclaw cron add --cron "0 10 * * *" \
  --name "my-project-4am" \
  --message "timeout 3600 /path/to/cowork run --project /abs/path/to/my-project --skill-dir /abs/path/to/skill --forever" \
  --session isolated --announce --to <your-chat-id> --json | jq -r '.id')

ID2=$(openclaw cron add --cron "0 12 * * *" \
  --name "my-project-6am" \
  --message "timeout 3600 /path/to/cowork run --project /abs/path/to/my-project --skill-dir /abs/path/to/skill --forever" \
  --session isolated --announce --to <your-chat-id> --json | jq -r '.id')

cowork set-crons --project ./my-project $ID1 $ID2
```

When the project completes, `cowork` automatically cancels all registered crons and announces completion. No manual cleanup needed.

**4. Set up a question-check cron** — run crons only fire 1-2x per day, but questions can be raised at any time. A frequent question-check cron surfaces them to you quickly:
```bash
openclaw cron add --cron "0 */3 * * *" \
  --name "my-project-questions" \
  --message "Check /abs/path/to/my-project for open questions using 'cowork question list --project /abs/path/to/my-project'. If any unanswered questions exist, message the user with the question text, options, and recommendation. If none, do nothing." \
  --session isolated --announce --to <your-chat-id>
```

**5. Run manually for the first time (optional, to seed initial tasks):**
```bash
cowork run \
  --project ./my-project \
  --skill-dir <path-to-this-skill> \
  --forever
```

`--skill-dir` points to this skill's directory so `cowork run` can find the agent scripts in `assets/scripts/`.

## What Happens on Each Run

**No tasks in queue → orchestrator fires:**
- First run: reads OVERVIEW.md, creates plan structure, seeds initial research tasks
- Subsequent runs: reviews completed work in `work/TASK-XXX/`, processes OUTPUT.md Discoveries sections, integrates specs into `plan/features/`, queues next tasks
- Binary archives completed task files to `history/` after orchestrator exits

**Tasks in queue → workers fire:**
- cowork runs up to `--workers` tasks in parallel using a native Go worker pool (default: 3)
- Workers complete → OUTPUT.md lands in `work/TASK-XXX/` — stays there until next orchestrator run
- Completed tasks removed from queue; files preserved for orchestrator review

**Scheduled passes (run automatically based on --pm-every / --architect-every):**
- **PM pass** (every 5 runs, default): reviews `plan/` accuracy, integrates research outputs, reprioritizes queue
- **Architect pass** (every 10 runs, default): reviews code structure, flags legitimate structural issues

## Manual Triggers

Force any step directly, bypassing scheduling:

```bash
cowork run --project . --skill-dir <skill-dir> --only orchestrator
cowork run --project . --skill-dir <skill-dir> --only pm
cowork run --project . --skill-dir <skill-dir> --only architect
cowork run --project . --skill-dir <skill-dir> --only worker --task TASK-004
```

## Question / Answer Flow

This is the core human-in-the-loop feedback mechanism. Workers raise questions for
genuinely blocking decisions — things they can't resolve defensibly on their own.

**Workers raise questions like this:**
```bash
cowork question create \
  --question "Should we use approach A or B?" \
  --options "A: description + tradeoffs; B: description + tradeoffs" \
  --recommendation "A — rationale"
```

**What happens next:**
1. Question saved to `questions/QUESTION-XXX.md`
2. The `--forever` loop detects it on the next iteration, prints the question to stdout, and exits — the project is blocked
3. The run cron's `--announce` flag delivers that output to you
4. Your question-check cron (step 4 above) also catches it between runs

**To answer:**
```bash
cowork decision submit --question-id QUESTION-001 --answer "A" --rationale "..."
```

The next run automatically picks up the decision and unblocks. No other action needed.

**To see all open questions:**
```bash
cowork question list --project ./my-project
```

The next `cowork run` unblocks automatically.

## Project Completion

When `cowork run --forever` detects the project is done (orchestrator runs and produces no new tasks or questions), it:

1. Sets `state.json` → `phase: complete`
2. Prints a completion banner to stdout (captured by `--announce`)
3. Cancels all crons registered via `cowork set-crons`

**Registering crons for auto-cancel:**
```bash
# PUT semantics — replaces any previously registered IDs
cowork set-crons --project ./my-project <id1> <id2> ...

# Clear all registered crons
cowork set-crons --project ./my-project
```

`state.json` stores the IDs in `cronIds`. Calling `set-crons` again replaces the list entirely.

**Checking project state:**
```bash
cat my-project/state.json   # phase will be "complete", "working", "blocked", or "idle"
```

## Key Flags

| Flag | Default | Notes |
|------|---------|-------|
| `--project` | `.` | Project root directory |
| `--skill-dir` | — | Path to this skill (for agent scripts) |
| `--workers` | `3` | Max parallel workers |
| `--timeout` | `60m` | Worker session timeout |
| `--forever` | off | Loop continuously until complete |
| `--pm-every` | `5` | PM pass every N orchestrator runs |
| `--architect-every` | `10` | Architect pass every N orchestrator runs |
| `--only` | — | Force one step: `orchestrator\|pm\|architect\|worker` |
| `--task` | — | Task ID for `--only worker` |

## Project Structure

```
my-project/
├── OVERVIEW.md          # Vision doc (human-written, copied from --from)
├── state.json           # Binary-managed: run counter, task counter, phase, cronIds
├── plan/
│   ├── OVERVIEW.md      # Synthesized architecture (agent-written)
│   └── features/        # Per-feature specs (agent-written)
├── work/TASK-XXX/       # Active task dirs — BRIEF.md + worker output
├── queue/todo.md        # Ready task queue (binary-managed)
├── done.md              # Completed task log with summaries
├── questions/           # Open questions awaiting human answers
├── decisions/           # Submitted answers
├── updates/             # Human-readable run summaries (agent-written)
├── log/                 # Structured JSON run logs (binary-managed)
└── history/             # Archived worker output files
```

## Agent Scripts

Located in `assets/scripts/`. Injected as prompts into spawned agent sessions:

- `cowork-cli.md` — CLI reference prepended to every session
- `orchestrator.md` — standard orchestrator boot instructions
- `pm-pass.md` — PM pass boot instructions
- `architect-pass.md` — architect pass boot instructions
- `CONSTRUCTOR.md` — worker boot instructions (used by cowork's internal worker pool)
