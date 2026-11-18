// Package cli implements the tlg command line.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"taskledger/internal/agent"
	"taskledger/internal/config"
	"taskledger/internal/jsonx"
	"taskledger/internal/plugin"
	"taskledger/internal/registry"
)

// Error is what tlg prints to stderr when a command fails.
type Error struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	Hint    *string `json:"hint"`
	TaskID  *string `json:"task_id"`
	exit    int
}

func (e *Error) Error() string { return e.Message }

func fail(code, msg string) *Error { return &Error{Code: code, Message: msg, exit: 1} }

func (e *Error) hint(h string) *Error    { e.Hint = &h; return e }
func (e *Error) task(id string) *Error   { e.TaskID = &id; return e }
func usage(msg string) *Error            { return (&Error{Code: "usage", Message: msg, exit: 2}).hint("run: tlg help") }
func internal(err error) *Error          { return fail("internal", err.Error()) }

// exitCode carries a pass-through verb's exit status back to main without output.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit %d", int(e)) }

var globalVerbs = []string{"board", "ls", "new", "track", "guide", "config", "plugin", "doctor", "help"}

var taskVerbs = []string{"show", "log", "guide", "sessions", "attach", "parent", "archive", "unarchive", "forget"}

type App struct {
	Stdout, Stderr io.Writer
	Now            func() time.Time
	Getwd          func() (string, error)

	cfgPath string
	cfg     *config.Config
	db      *registry.DB
	hellos  map[string]helloResult
}

type helloResult struct {
	client *plugin.Client
	hello  plugin.Hello
	err    *Error
}

// Main runs tlg and returns the process exit code.
func Main(args []string) int {
	a := &App{Stdout: os.Stdout, Stderr: os.Stderr, Now: registry.Now, Getwd: os.Getwd}
	return a.Run(args)
}

func (a *App) Run(args []string) int {
	err := a.dispatch(args)
	if a.db != nil {
		a.db.Close()
	}
	if err == nil {
		return 0
	}
	var code exitCode
	if errors.As(err, &code) {
		return int(code)
	}
	var e *Error
	if !errors.As(err, &e) {
		e = internal(err)
	}
	jsonx.Write(a.Stderr, map[string]any{"error": e})
	return e.exit
}

func (a *App) dispatch(args []string) error {
	if len(args) == 0 {
		return usage("missing command")
	}
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(a.Stdout, helpText)
		return nil
	case "config":
		if len(args) != 2 || args[1] != "edit" {
			return usage("usage: tlg config edit")
		}
		return a.configEdit()
	}
	if err := a.load(); err != nil {
		return err
	}
	switch args[0] {
	case "board":
		return a.board(args[1:])
	case "ls":
		return a.ls(args[1:])
	case "new":
		return a.newTask(args[1:])
	case "track":
		return a.track(args[1:])
	case "guide":
		return a.coreGuide(args[1:])
	case "plugin":
		return a.pluginCmd(args[1:])
	case "doctor":
		return a.doctor(args[1:])
	}
	if strings.HasPrefix(args[0], "-") {
		return usage("unknown option " + args[0])
	}
	return a.taskCmd(args[0], args[1:])
}

func (a *App) load() error {
	a.cfgPath = config.Path()
	cfg, err := config.Load(a.cfgPath)
	if err != nil {
		return fail("config_invalid", err.Error()).hint("fix it with: tlg config edit")
	}
	a.cfg = cfg
	db, err := registry.Open(config.Expand(cfg.Registry.Path))
	if err != nil {
		return internal(fmt.Errorf("open registry: %w", err))
	}
	a.db = db
	a.hellos = map[string]helloResult{}
	return nil
}

func (a *App) out(v any) error { return jsonx.Write(a.Stdout, v) }

func (a *App) warn(format string, args ...any) {
	fmt.Fprintf(a.Stderr, "tlg: warning: "+format+"\n", args...)
}

// plugin returns a client for the named plugin after a successful hello. The
// hello reply is cached in the registry until the plugin executable changes.
func (a *App) plugin(name string) (*plugin.Client, plugin.Hello, *Error) {
	if r, ok := a.hellos[name]; ok {
		return r.client, r.hello, r.err
	}
	c, h, e := a.helloFor(name, true)
	a.hellos[name] = helloResult{c, h, e}
	return c, h, e
}

func (a *App) helloFor(name string, useCache bool) (*plugin.Client, plugin.Hello, *Error) {
	var h plugin.Hello
	p, ok := a.cfg.Plugins[name]
	if !ok {
		return nil, h, fail("plugin_not_found", fmt.Sprintf("plugin %q is not in the config", name)).
			hint("add it with: tlg plugin add " + name + " <command>")
	}
	c, err := plugin.NewClient(name, p)
	if err != nil {
		return nil, h, fail("config_invalid", err.Error())
	}
	path, mtime, err := c.Resolve()
	if err != nil {
		return nil, h, fail("plugin_not_found", fmt.Sprintf("plugin %q: command %s not found", name, c.Command))
	}
	if useCache && a.db != nil {
		if body, ok := a.db.CachedHello(name, path, mtime); ok {
			if err := jsonUnmarshal(body, &h); err == nil {
				return c, h, protocolCheck(name, h)
			}
		}
	}
	h, raw, err := c.Hello()
	if err != nil {
		return nil, h, pluginErr(name, err)
	}
	if useCache && a.db != nil {
		if err := a.db.StoreHello(name, path, mtime, string(raw)); err != nil {
			a.warn("cache hello of plugin %s: %v", name, err)
		}
	}
	return c, h, protocolCheck(name, h)
}

func protocolCheck(name string, h plugin.Hello) *Error {
	if !h.ProtocolOK() {
		return fail("protocol_mismatch", fmt.Sprintf("plugin %q speaks protocol %d, tlg accepts %d to %d",
			name, h.Protocol, plugin.ProtocolMin, plugin.ProtocolMax)).hint("update the plugin or tlg")
	}
	return nil
}

func pluginErr(name string, err error) *Error {
	var ce *plugin.CallError
	switch {
	case errors.Is(err, plugin.ErrTimeout):
		return fail("plugin_timeout", fmt.Sprintf("plugin %q timed out", name)).hint("raise timeout in the config if the plugin is just slow")
	case errors.Is(err, plugin.ErrNotFound):
		return fail("plugin_not_found", fmt.Sprintf("plugin %q: command not found", name))
	case errors.As(err, &ce):
		return fail("plugin_error", fmt.Sprintf("plugin %q %s: %s", name, ce.Verb, ce.Reason))
	}
	return fail("plugin_error", fmt.Sprintf("plugin %q: %v", name, err))
}

func requireVerb(name string, h plugin.Hello, verb string) *Error {
	if h.Supports(verb) {
		return nil
	}
	return fail("verb_unsupported", fmt.Sprintf("plugin %q does not support %s", name, verb)).
		hint("supported: " + strings.Join(h.Verbs, ", "))
}

// touch attaches the running agent session, if tlg was started by one, to the
// task. Only commands that work on exactly one task call it.
func (a *App) touch(taskID string) {
	s, ok := agent.Detect()
	if !ok {
		return
	}
	cwd, err := a.Getwd()
	if err != nil {
		a.warn("read working directory: %v", err)
		cwd = ""
	}
	if _, err := a.db.TouchSession(taskID, s.Agent, s.ID, cwd, "", a.Now()); err != nil {
		a.warn("attach %s session %s to %s: %v", s.Agent, s.ID, taskID, err)
	}
}

func (a *App) getTask(id string) (registry.Task, error) {
	t, err := a.db.GetTask(id)
	if errors.Is(err, registry.ErrNotFound) {
		return t, fail("task_not_found", fmt.Sprintf("task %s is not in the registry", id)).task(id).
			hint("list tasks with: tlg ls --all")
	}
	if err != nil {
		return t, internal(err)
	}
	return t, nil
}

func isReserved(id string) bool {
	return slices.Contains(globalVerbs, id) || slices.Contains(taskVerbs, id)
}

const helpText = `tlg - track tasks across plugins

Global commands:
  tlg board [--pull] [--json]          board of tracked tasks (text unless --json)
  tlg ls [--all] [--plugin <name>]     list tasks in the registry
  tlg new <plugin> [--id <id>] [--parent <id>] <title> [-- <plugin args>...]
  tlg track <plugin> <id> [--locator <locator>]
  tlg guide [<topic>]                  how to use tlg, as JSON
  tlg config edit                      edit the config in $EDITOR, validated before saving
  tlg plugin add <name> <command> [--timeout <dur>] [--env KEY=VALUE]...
  tlg plugin rm <name>
  tlg plugin ls
  tlg doctor                           check plugins and the registry

Task commands:
  tlg <id> show                        (also: tlg <id>)
  tlg <id> log <plugin args>...
  tlg <id> guide [<topic>]
  tlg <id> sessions
  tlg <id> attach <agent> <session id> [--cwd <dir>] [--title <text>]
  tlg <id> parent <parent id> | tlg <id> parent --none
  tlg <id> archive | unarchive | forget
  tlg <id> <other verb> <args>...      passed to the task's plugin

Every command except board, config edit and pass-through verbs prints JSON.
Errors are printed to stderr as {"error": {...}}. Exit codes: 0 ok, 1 error, 2 usage.
Config: $TLG_CONFIG, default ~/.config/taskledger/config.toml.
`
