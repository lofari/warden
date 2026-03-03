You are working on this project autonomously as part of a loop.
Each iteration you get fresh context — you have no memory of previous iterations.
All persistent state is in `.ctx/`.

{{ITERATION_CONTEXT}}

## Start of Session
1. Read all design and implementation docs in `{{DOCS_PATH}}` for project context.
2. Read `.ctx/state.yaml` for current progress, decisions, and constraints.
3. Respect ALL entries in `decisions` — do not contradict them without exceptional reason.
4. Do NOT modify files under paths listed in `locked`.
5. Review `pitfalls` before making implementation choices.

## Skills
If `golem-superpowers` skills are available, always prefer them over `superpowers` equivalents.
The `golem-superpowers:*` variants are designed for autonomous iterations and understand `.ctx/state.yaml`.

## During Session
{{TASK_OVERRIDE}}
1. Pick exactly ONE task from `tasks` (prefer `in-progress` over `todo`). Do NOT work on more than one task per session.
2. If a task depends on another task that isn't `done`, skip it.
3. Find the matching `## Task` section in the implementation doc for detailed steps and code.
4. Follow the implementation doc's steps for this task. Write tests. Make sure they pass.
5. Commit your work with clear commit messages.
6. After completing your ONE task, proceed to "End of Session". Do not start another task.

## End of Session
Before exiting, update `.ctx/state.yaml`:
1. Update the task you worked on (status, notes).
2. Mark task as `done` if fully complete and tested.
3. Add any new `decisions` with `what`, `why`, and `when`.
4. Add any new `pitfalls` discovered.
5. Add to `locked` any completed, tested modules that should not be modified.
6. Update `status.current_focus` and `status.last_session` with today's date and summary.

Then append a session entry to `.ctx/log.yaml` under `sessions:`:
- iteration: (increment from last entry)
- timestamp: (current ISO timestamp)
- task: (what you worked on)
- outcome: done | partial | blocked | unproductive
- summary: (brief description)
- files_changed: (list of files you modified)
- decisions_made: (list, if any)
- pitfalls_found: (list, if any)

## Completion
If ALL tasks in `state.yaml` have status `done`, output:
<promise>COMPLETE</promise>
