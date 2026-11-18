package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"taskledger/internal/board"
	"taskledger/internal/config"
	"taskledger/internal/registry"
)

// boardWorkers bounds how many plugin processes run at once while building the board.
const boardWorkers = 8

func (a *App) board(args []string) error {
	p, err := parseArgs(args, map[string]bool{"pull": false, "json": false})
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return usage("usage: tlg board [--pull] [--json]")
	}
	all, err := a.db.ListTasks(true, "")
	if err != nil {
		return internal(err)
	}
	archived := map[string]bool{}
	var tracked []registry.Task
	for _, t := range all {
		if t.Registry == registry.Archived {
			archived[t.ID] = true
		} else {
			tracked = append(tracked, t)
		}
	}
	// Hellos run once per plugin before the fan-out, so workers only read the cache.
	for _, t := range tracked {
		a.plugin(t.Plugin)
	}
	items := make([]board.Item, len(tracked))
	var wg sync.WaitGroup
	sem := make(chan struct{}, boardWorkers)
	for i, t := range tracked {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items[i] = a.boardItem(t, p.has("pull"))
		}()
	}
	wg.Wait()
	for i := range items {
		if s := items[i].Status; s != nil {
			a.cacheTitle(&items[i].Task, s.Title)
		}
	}
	b := board.Build(items, archived, a.Now())
	if p.has("json") {
		return a.out(b)
	}
	board.Render(a.Stdout, b, len(tracked))
	return nil
}

func (a *App) boardItem(t registry.Task, pull bool) board.Item {
	it := board.Item{Task: t}
	r := a.hellos[t.Plugin]
	if r.err != nil {
		switch r.err.Code {
		case "plugin_not_found":
			it.Flags = append(it.Flags, "plugin not found")
		case "protocol_mismatch":
			it.Flags = append(it.Flags, "plugin needs update")
		default:
			it.Flags = append(it.Flags, "plugin error: "+r.err.Message)
		}
		return it
	}
	if pull && r.hello.Supports("pull") {
		res, err := r.client.Pull(ref(t))
		switch {
		case err != nil:
			it.Flags = append(it.Flags, "pull failed: "+pluginErr(t.Plugin, err).Message)
		case !res.OK && res.Error != nil:
			it.Flags = append(it.Flags, "pull failed: "+*res.Error)
		case !res.OK:
			it.Flags = append(it.Flags, "pull failed")
		}
	}
	s, err := r.client.Status(ref(t))
	if err != nil {
		it.Flags = append(it.Flags, "plugin error: "+pluginErr(t.Plugin, err).Message)
		return it
	}
	it.Status = &s
	return it
}

type pluginCheck struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	OK       bool     `json:"ok"`
	Protocol *int     `json:"protocol"`
	Verbs    []string `json:"verbs"`
	Children *bool    `json:"children"`
	Error    *string  `json:"error"`
}

func (a *App) checkPlugin(name string) pluginCheck {
	pc := pluginCheck{Name: name, Command: a.cfg.Plugins[name].Command, Verbs: []string{}}
	_, h, e := a.helloFor(name, false)
	if h.Verbs != nil {
		proto, children := h.Protocol, h.Children
		pc.Protocol, pc.Children, pc.Verbs = &proto, &children, h.Verbs
	}
	if e != nil {
		msg := e.Message
		pc.Error = &msg
		return pc
	}
	pc.OK = true
	return pc
}

func (a *App) checkAll() []pluginCheck {
	names := make([]string, 0, len(a.cfg.Plugins))
	for n := range a.cfg.Plugins {
		names = append(names, n)
	}
	sort.Strings(names)
	out := []pluginCheck{}
	for _, n := range names {
		out = append(out, a.checkPlugin(n))
	}
	return out
}

func (a *App) pluginCmd(args []string) error {
	if len(args) == 0 {
		return usage("usage: tlg plugin add|rm|ls")
	}
	switch args[0] {
	case "ls":
		if len(args) != 1 {
			return usage("usage: tlg plugin ls")
		}
		return a.doctor(nil)
	case "add":
		return a.pluginAdd(args[1:])
	case "rm":
		if len(args) != 2 {
			return usage("usage: tlg plugin rm <name>")
		}
		name := args[1]
		if _, ok := a.cfg.Plugins[name]; !ok {
			return fail("plugin_not_found", fmt.Sprintf("plugin %q is not in the config", name))
		}
		delete(a.cfg.Plugins, name)
		if err := config.Save(a.cfgPath, a.cfg); err != nil {
			return internal(err)
		}
		return a.out(map[string]any{"name": name})
	}
	return usage("usage: tlg plugin add|rm|ls")
}

func (a *App) pluginAdd(args []string) error {
	p, err := parseArgs(args, map[string]bool{"timeout": true, "env": true})
	if err != nil {
		return err
	}
	if len(p.pos) != 2 {
		return usage("usage: tlg plugin add <name> <command> [--timeout <dur>] [--env KEY=VALUE]...")
	}
	name, command := p.pos[0], p.pos[1]
	if !config.ValidPluginName(name) {
		return fail("config_invalid", fmt.Sprintf("plugin name %q: use lowercase letters, digits and dashes", name))
	}
	if _, ok := a.cfg.Plugins[name]; ok {
		return fail("config_invalid", fmt.Sprintf("plugin %q already exists", name)).hint("remove it first with: tlg plugin rm " + name)
	}
	pl := config.Plugin{Command: command}
	pl.Timeout, _ = p.get("timeout")
	for _, kv := range p.flags["env"] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return usage("--env takes KEY=VALUE, got " + quote(kv))
		}
		if pl.Env == nil {
			pl.Env = map[string]string{}
		}
		pl.Env[k] = v
	}
	if _, err := pl.TimeoutDuration(); err != nil {
		return fail("config_invalid", err.Error())
	}
	a.cfg.Plugins[name] = pl
	pc := a.checkPlugin(name)
	if !pc.OK {
		return fail("config_invalid", "plugin check failed, config not changed: "+*pc.Error)
	}
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return internal(err)
	}
	return a.out(map[string]any{"plugin": pc})
}

func (a *App) doctor(args []string) error {
	if len(args) > 0 {
		return usage("usage: tlg doctor")
	}
	checks := a.checkAll()
	ok := true
	for _, c := range checks {
		ok = ok && c.OK
	}
	tasks, err := a.db.ListTasks(true, "")
	if err != nil {
		return internal(err)
	}
	orphans := []string{}
	for _, t := range tasks {
		if _, has := a.cfg.Plugins[t.Plugin]; !has {
			orphans = append(orphans, t.ID)
		}
	}
	return a.out(map[string]any{"plugins": checks, "orphan_tasks": orphans, "ok": ok && len(orphans) == 0})
}

// configEdit works like visudo: the user edits a copy, and the real file is only
// replaced once the copy parses and every plugin answers hello.
func (a *App) configEdit() error {
	path := config.Path()
	orig, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		orig = []byte(configTemplate)
	} else if err != nil {
		return internal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return internal(err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".edit-*.toml")
	if err != nil {
		return internal(err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(orig); err != nil {
		return internal(err)
	}
	tmp.Close()
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fail("usage", "config edit needs a terminal").hint("agents should use: tlg plugin add|rm")
	}
	defer tty.Close()
	in := bufio.NewReader(tty)
	for {
		if err := runEditor(tmp.Name(), tty); err != nil {
			return internal(err)
		}
		data, err := os.ReadFile(tmp.Name())
		if err != nil {
			return internal(err)
		}
		problems := a.validateConfig(data)
		if len(problems) == 0 {
			if err := config.WriteAtomic(path, data); err != nil {
				return internal(err)
			}
			fmt.Fprintf(a.Stdout, "saved %s\n", path)
			return nil
		}
		fmt.Fprintln(tty, "config not saved:")
		for _, pr := range problems {
			fmt.Fprintln(tty, "  - "+pr)
		}
		fmt.Fprint(tty, "[e]dit again or [a]bort? ")
		answer, _ := in.ReadString('\n')
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(answer)), "e") {
			return fail("config_invalid", "edit aborted, config not changed")
		}
	}
}

func (a *App) validateConfig(data []byte) []string {
	c := &config.Config{}
	if err := config.Parse(data, c); err != nil {
		return []string{err.Error()}
	}
	saved := a.cfg
	a.cfg = c
	defer func() { a.cfg = saved }()
	var problems []string
	for _, pc := range a.checkAll() {
		if !pc.OK {
			problems = append(problems, *pc.Error)
		}
	}
	return problems
}

func runEditor(path string, tty *os.File) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "editor", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	return cmd.Run()
}

const configTemplate = `[registry]
path = "~/.local/share/taskledger/registry.db"

# [plugins.example]
# command = "~/path/to/plugin"
# env = { ROOT = "~/data" }
# timeout = "15s"
`

