<!-- golem:start -->
## Context Engineering (auto-managed by golem)

This project uses `.ctx/` for persistent state across sessions.

- **Design & implementation docs** — See `project.docs_path` in `.ctx/state.yaml` for location. Read ALL docs for project intent and architecture.
- **`.ctx/state.yaml`** — Current state: tasks, decisions, locked paths, pitfalls. Read at start, update at end of every session.
- **`.ctx/log.yaml`** — Session history. Append an entry at the end of every session.

### Task Mapping Convention
Each `## Task` section in the implementation doc must have a corresponding entry in `state.yaml` tasks. Use the task title from the implementation doc as the task name in state.yaml. The implementation doc contains the detailed steps and code — state.yaml tracks progress.

When planning: create tasks in state.yaml that match 1:1 with the implementation doc sections.
When building: find the matching implementation doc section for your current task and follow its steps.

### Rules
- Respect all `decisions` in state.yaml — do not contradict without exceptional reason.
- Do not modify files under `locked` paths.
- Check `pitfalls` before implementation choices.
- Update state.yaml and log.yaml at the end of every session.
<!-- golem:end -->
