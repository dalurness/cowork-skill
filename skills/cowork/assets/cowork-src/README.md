# cowork

Async project manager for AI agents. A deterministic backbone for long-running,
multi-step AI projects — workers execute tasks in parallel, an orchestrator reviews
output and queues next steps, and PM/Architect passes run on a schedule.

Designed for use with [OpenClaw](https://openclaw.ai) but the binary works standalone
with any agent setup that can run CLI commands.

## Quick Install

Requires Go 1.20+ and an agent CLI (`claude`, `codex`, `opencode`, etc.).
See `cmd/workers.go` to configure the command for your tooling — defaults to `claude`.

```bash
git clone git@github.com:dalurness/cowork-skill.git
cd cowork-skill
chmod +x install.sh && ./install.sh
```

This builds the `cowork` binary and installs it to `~/bin/cowork`.

### Register as an OpenClaw Skill

```bash
openclaw skills add /path/to/cowork-skill
```

Once registered, agents can use `cowork` via the skill instructions in `SKILL.md`.

### go install

```bash
go install github.com/dalurness/cowork-skill@latest
```

Installs the binary only. You'll still need to point `--skill-dir` at a local clone
of this repo for the agent scripts in `assets/scripts/`.

## Repository Layout

```
SKILL.md                ← OpenClaw skill instructions and CLI reference
README.md
install.sh              ← builds binary + prints skill registration command
main.go                 ← Go source (binary entry point)
go.mod / go.sum
cmd/                    ← CLI commands
state/                  ← state management
templates/
  BRIEF.md              ← task brief format reference
assets/
  scripts/              ← agent prompts (orchestrator, worker, PM, architect)
```

## How It Works

**Each run does one of two things:**

- **Tasks in queue → workers fire** — up to 3 parallel agent sessions, each
  working a task in isolation. Output lands in `work/TASK-XXX/` for the orchestrator.

- **No tasks in queue → orchestrator fires** — reviews completed work, integrates
  outputs into `plan/`, queues next tasks. PM and Architect passes run on a schedule.

**Cron-driven:** schedule `cowork run --forever` via `openclaw cron` and the project
runs autonomously. The binary auto-cancels crons when the project completes.

## Feedback Loop — Questions & Decisions

cowork is designed for long-running projects where human judgment is occasionally
needed. When a worker hits a blocking fork it can't resolve on its own, it raises
a question and the project pauses until you answer.

### How questions surface

1. A worker calls `cowork question create` with the question, options, and its recommendation
2. The question is saved to `questions/QUESTION-XXX.md`
3. On the next run, `cowork run --forever` detects the open question, prints a summary
   to stdout, and exits — the project is blocked until answered
4. If you have `--announce` on your run cron, that output is delivered to you automatically

**The gap:** if a question is raised between scheduled runs, you won't see it until
the next cron fires (potentially 12-24h later). To close this, set up a dedicated
question-check cron that runs every few hours:

```bash
# Check all active projects for open questions every 3 hours
openclaw cron add --cron "0 */3 * * *" \
  --name "cowork-question-check" \
  --message "Check all cowork projects under ~/.../projects/ for open questions using
'cowork question list --project <path>'. If any unanswered questions exist, surface
them to the user with the project name, question text, options, and recommendation.
If no questions are pending across all projects, do nothing." \
  --session isolated --announce --to <your-chat-id>
```

### Answering a question

```bash
cowork decision submit \
  --question-id QUESTION-001 \
  --answer "A" \
  --rationale "We prefer approach A because..."
```

The next run automatically picks up the decision and unblocks. No manual intervention
needed beyond submitting the answer.

### Viewing open questions

```bash
cowork question list --project ./my-project
```

## Usage

```bash
# Initialize a project from an overview directory
cowork init --project ./my-project --from ./my-notes

# Run (one cycle)
cowork run --project ./my-project --skill-dir /path/to/cowork-skill

# Run continuously until complete (use with timeout for cron)
timeout 3600 cowork run --project ./my-project --skill-dir /path/to/cowork-skill --forever

# Manually trigger a specific step
cowork run --project . --skill-dir /path/to/cowork-skill --only orchestrator
cowork run --project . --skill-dir /path/to/cowork-skill --only worker --task TASK-004

# Answer a blocking question
cowork decision submit --question-id QUESTION-001 --answer "A" --rationale "..."
```

See `SKILL.md` for the full CLI reference and project structure documentation.
