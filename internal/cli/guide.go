package cli

import (
	"taskledger/internal/plugin"
)

// coreGuides is what an agent reads to learn tlg. The default entry stays short:
// it holds only the rules an agent must follow on every task, and points at
// topics for the rest.
var coreGuides = map[string]plugin.Guide{
	"": {
		Rules: []string{
			"Work on one task per session, and always name it by task id. Never infer the task from the current directory or git branch.",
			"At the start of a session run: tlg <id> show, then tlg <id> guide. Report where the task is in a few lines; do not retell its history.",
			"Record every milestone with tlg <id> log right when it happens. A milestone that is not logged does not exist for the next session.",
			"Call plugin commands through tlg (tlg <id> <verb> ...), never by running the plugin directly, so the session is attached to the task.",
			"After tlg board, print the board unchanged, then suggest at most three tasks to work on next and why. Lines marked * are the ones that need action.",
			"Never run tlg config edit: it waits for a keyboard. Use tlg plugin add|rm|ls instead.",
		},
		Topics: []plugin.GuideTopic{
			{Name: "commands", When: "you need the full command list and their arguments"},
			{Name: "board", When: "you need to read groups, markers or flags on the board"},
			{Name: "sessions", When: "the user asks which agent sessions worked on a task, or wants to resume one"},
			{Name: "tasks", When: "you create, track, archive, re-parent or forget a task"},
			{Name: "errors", When: "a tlg command failed and you need to act on its error code"},
		},
	},
	"commands": {
		Sections: []plugin.GuideSection{
			{Title: "global", Items: []string{
				"tlg board [--pull] [--json]: text board of tracked tasks; --pull refreshes external data first.",
				"tlg ls [--all] [--plugin <name>]: tasks in the registry; --all includes archived ones.",
				"tlg new <plugin> [--id <id>] [--parent <id>] <title> [-- <plugin args>...]",
				"tlg track <plugin> <id> [--locator <locator>]: register a task that already exists in a plugin.",
				"tlg guide [<topic>]: this guide.",
				"tlg plugin add <name> <command> [--timeout <dur>] [--env KEY=VALUE]... | plugin rm <name> | plugin ls",
				"tlg doctor: check every plugin and find tasks whose plugin is gone.",
			}},
			{Title: "per task", Items: []string{
				"tlg <id> show: start-of-session summary.",
				"tlg <id> log <plugin args>...: append a history entry; the plugin defines the arguments, see tlg <id> guide.",
				"tlg <id> guide [<topic>]: the plugin's instructions for the task's current stage.",
				"tlg <id> sessions | attach <agent> <session id> [--cwd <dir>] [--title <text>]",
				"tlg <id> parent <parent id> | parent --none",
				"tlg <id> archive | unarchive | forget",
				"tlg <id> <other verb> ...: passed unchanged to the task's plugin; output is the plugin's own.",
			}},
		},
	},
	"board": {
		Sections: []plugin.GuideSection{
			{Title: "groups", Items: []string{
				"Groups in order: NEEDS ME, ACTIVE, WAITING, PAUSED, CLOSED.",
				"A whole parent/child tree sits in the most urgent group found among its tasks.",
				"A line starting with * has its own state equal to its group: that is the task to act on. Unmarked lines are context.",
				"A task whose plugin failed is in NEEDS ME with a flag saying why.",
			}},
			{Title: "columns", Items: []string{
				"marker, id (indented by depth), plugin, title, stage, last activity, idle time, --> next action, [due and flags].",
				"stage, next and plugin flags are the plugin's words, printed verbatim.",
			}},
			{Title: "core flags", Items: []string{
				"all children closed: the parent may be ready to close.",
				"parent archived: the parent left the board, this task is shown on its own.",
				"overdue: due date passed and the task is not closed.",
				"plugin not found / plugin needs update / plugin error: ... / pull failed: ...: fix the plugin or config, then run tlg doctor.",
			}},
		},
	},
	"sessions": {
		Sections: []plugin.GuideSection{
			{Title: "recording", Items: []string{
				"Sessions are attached automatically when an agent runs new, show, log, guide or a pass-through verb on a task.",
				"Automatic detection currently knows Claude Code only. Other agents attach by hand: tlg <id> attach <agent> <session id>.",
				"tlg board and tlg ls never attach a session.",
			}},
			{Title: "resuming", Items: []string{
				"tlg <id> sessions lists agent, session_id and cwd, newest first. Resume it with that agent's own resume command from that cwd.",
			}},
		},
	},
	"tasks": {
		Sections: []plugin.GuideSection{
			{Title: "ids", Items: []string{
				"Task ids are unique across all plugins: ASCII letters, digits, '_', '-', '.', starting with a letter or digit, never a tlg command name.",
				"Without --id the plugin proposes one. A child without --id defaults to <parent id>.<short title>.",
			}},
			{Title: "registry states", Items: []string{
				"tracked: on the board. archived: off the board, still in tlg ls --all, sessions kept, unarchive brings it back.",
				"Archive only when the user asks. forget removes the task and its sessions from the registry; use it only for tasks added by mistake.",
				"Nothing in tlg deletes a task's files.",
			}},
			{Title: "parents", Items: []string{
				"One parent per task, any depth, parent and child may belong to different plugins. tlg refuses cycles.",
				"A parent's state comes from its own plugin; tlg never derives it from the children.",
			}},
		},
	},
	"errors": {
		Sections: []plugin.GuideSection{
			{Title: "format", Items: []string{
				"On failure tlg prints {\"error\": {code, message, hint, task_id}} to stderr and nothing to stdout. Exit 1 for errors, 2 for usage.",
				"Branch on error.code, show error.message to the user, follow error.hint when present.",
			}},
			{Title: "codes", Items: []string{
				"usage, task_not_found, duplicate_id, invalid_id, cycle, plugin_not_found, plugin_error, plugin_timeout, protocol_mismatch, verb_unsupported, log_rejected, not_located, config_invalid, internal.",
				"log_rejected: the plugin refused the entry; message says why. Fix the arguments, do not retry unchanged.",
			}},
		},
	},
}

func (a *App) coreGuide(args []string) error {
	if len(args) > 1 {
		return usage("usage: tlg guide [<topic>]")
	}
	key := ""
	if len(args) == 1 {
		key = args[0]
	}
	g, ok := coreGuides[key]
	if !ok {
		names := []string{}
		for _, t := range coreGuides[""].Topics {
			names = append(names, t.Name)
		}
		return usage("unknown guide topic " + quote(key)).hint("topics: " + joinWords(names))
	}
	if key != "" {
		g.Topic = &key
	}
	return a.out(guideResult{Scope: "core", TaskID: nil, Guide: g.Normalize()})
}
