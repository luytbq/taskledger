package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"taskledger/internal/plugin"
	"taskledger/internal/registry"
)

type taskResult struct {
	Task     registry.Task `json:"task"`
	Warnings []string      `json:"warnings"`
}

func ref(t registry.Task) plugin.TaskRef { return plugin.TaskRef{ID: t.ID, Locator: t.Locator} }

func (a *App) ls(args []string) error {
	p, err := parseArgs(args, map[string]bool{"all": false, "plugin": true})
	if err != nil {
		return err
	}
	if len(p.pos) > 0 {
		return usage("usage: tlg ls [--all] [--plugin <name>]")
	}
	name, _ := p.get("plugin")
	tasks, err := a.db.ListTasks(p.has("all"), name)
	if err != nil {
		return internal(err)
	}
	return a.out(map[string]any{"tasks": tasks})
}

func (a *App) newTask(args []string) error {
	p, err := parseArgs(args, map[string]bool{"id": true, "parent": true})
	if err != nil {
		return err
	}
	if len(p.pos) < 2 {
		return usage("usage: tlg new <plugin> [--id <id>] [--parent <id>] <title> [-- <plugin args>...]")
	}
	name, title := p.pos[0], joinWords(p.pos[1:])
	c, h, e := a.plugin(name)
	if e != nil {
		return e
	}
	in := plugin.NewInput{Title: title}
	var parent *registry.Task
	if pid, ok := p.get("parent"); ok {
		pt, err := a.getTask(pid)
		if err != nil {
			return err
		}
		parent = &pt
		if pt.Plugin == name {
			if !h.Children {
				return fail("verb_unsupported", fmt.Sprintf("plugin %q does not create child tasks", name)).task(pid)
			}
			r := ref(pt)
			in.Parent = &r
		}
	}
	id, hasID := p.get("id")
	if !hasID && parent != nil {
		if s := slug(title, 24); s != "" {
			id, hasID = parent.ID+"."+s, true
		}
	}
	if hasID {
		if e := a.checkNewID(id); e != nil {
			return e
		}
		in.ID = &id
	}
	res, err := c.New(in, p.rest)
	if err != nil {
		return pluginErr(name, err)
	}
	if e := a.checkNewID(res.ID); e != nil {
		return e.hint(fmt.Sprintf("plugin %s already created the task at %q; remove it there or track it under another id", name, res.Locator))
	}
	t := registry.Task{ID: res.ID, Plugin: name, Locator: res.Locator, Title: res.Title,
		Registry: registry.Tracked, CreatedAt: registry.Stamp(a.Now())}
	if parent != nil {
		t.ParentID = &parent.ID
	}
	if err := a.db.AddTask(t); err != nil {
		return a.addErr(t.ID, err)
	}
	a.touch(t.ID)
	return a.out(taskResult{Task: t, Warnings: []string{}})
}

func (a *App) checkNewID(id string) *Error {
	if e := checkID(id); e != nil {
		return e
	}
	exists, err := a.db.Exists(id)
	if err != nil {
		return internal(err)
	}
	if exists {
		return fail("duplicate_id", "task id "+id+" already exists").task(id).hint("use another id or run: tlg " + id + " show")
	}
	return nil
}

func (a *App) addErr(id string, err error) error {
	if errors.Is(err, registry.ErrDuplicate) {
		return fail("duplicate_id", "task id "+id+" already exists").task(id)
	}
	return internal(err)
}

func (a *App) track(args []string) error {
	p, err := parseArgs(args, map[string]bool{"locator": true})
	if err != nil {
		return err
	}
	if len(p.pos) != 2 {
		return usage("usage: tlg track <plugin> <id> [--locator <locator>]")
	}
	name, id := p.pos[0], p.pos[1]
	if e := a.checkNewID(id); e != nil {
		return e
	}
	c, h, e := a.plugin(name)
	if e != nil {
		return e
	}
	t := registry.Task{ID: id, Plugin: name, Registry: registry.Tracked, CreatedAt: registry.Stamp(a.Now())}
	if loc, ok := p.get("locator"); ok {
		t.Locator = loc
	} else {
		if e := requireVerb(name, h, "locate"); e != nil {
			return e.hint("pass --locator, or add locate to the plugin")
		}
		r, err := c.Locate(id)
		if err != nil {
			return pluginErr(name, err)
		}
		if !r.Found {
			return fail("not_located", fmt.Sprintf("plugin %q has no task %s", name, id)).task(id)
		}
		t.Locator = *r.Locator
		if r.Title != nil {
			t.Title = *r.Title
		}
	}
	if err := a.db.AddTask(t); err != nil {
		return a.addErr(id, err)
	}
	return a.out(taskResult{Task: t, Warnings: []string{}})
}

func (a *App) taskCmd(id string, args []string) error {
	t, err := a.getTask(id)
	if err != nil {
		return err
	}
	verb := "show"
	if len(args) > 0 {
		verb, args = args[0], args[1:]
	}
	switch verb {
	case "show":
		return a.show(t, args)
	case "log":
		return a.log(t, args)
	case "guide":
		return a.taskGuide(t, args)
	case "sessions":
		return a.sessions(t, args)
	case "attach":
		return a.attach(t, args)
	case "parent":
		return a.parent(t, args)
	case "archive":
		return a.archive(t, args)
	case "unarchive":
		return a.setRegistry(t, args, registry.Tracked, "unarchive")
	case "forget":
		return a.forget(t, args)
	}
	return a.passthrough(t, verb, args)
}

func noArgs(args []string, use string) error {
	if len(args) > 0 {
		return usage("usage: " + use)
	}
	return nil
}

type showResult struct {
	Task     registry.Task      `json:"task"`
	Show     plugin.Show        `json:"show"`
	Children []string           `json:"children"`
	Sessions []registry.Session `json:"sessions"`
	Flags    []string           `json:"flags"`
}

func (a *App) show(t registry.Task, args []string) error {
	if err := noArgs(args, "tlg <id> show"); err != nil {
		return err
	}
	c, _, e := a.plugin(t.Plugin)
	if e != nil {
		return e.task(t.ID)
	}
	a.touch(t.ID)
	s, err := c.Show(ref(t))
	if err != nil {
		return pluginErr(t.Plugin, err).task(t.ID)
	}
	a.cacheTitle(&t, s.Title)
	children, err := a.db.Children(t.ID, true)
	if err != nil {
		return internal(err)
	}
	sessions, err := a.db.Sessions(t.ID, 3)
	if err != nil {
		return internal(err)
	}
	flags := []string{}
	if t.ParentID != nil {
		if pt, err := a.db.GetTask(*t.ParentID); err == nil && pt.Registry == registry.Archived {
			flags = append(flags, "parent archived")
		}
	}
	if s.Due != nil && s.State != "closed" {
		if d, err := time.ParseInLocation("2006-01-02", *s.Due, registry.VN); err == nil && d.Before(dayStart(a.Now())) {
			flags = append(flags, "overdue")
		}
	}
	return a.out(showResult{Task: t, Show: s, Children: children, Sessions: sessions, Flags: flags})
}

func dayStart(t time.Time) time.Time {
	t = t.In(registry.VN)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, registry.VN)
}

func (a *App) cacheTitle(t *registry.Task, title string) {
	if title == "" || title == t.Title {
		return
	}
	if err := a.db.SetTitle(t.ID, title); err != nil {
		a.warn("update title of %s: %v", t.ID, err)
		return
	}
	t.Title = title
}

func (a *App) log(t registry.Task, args []string) error {
	c, _, e := a.plugin(t.Plugin)
	if e != nil {
		return e.task(t.ID)
	}
	a.touch(t.ID)
	r, err := c.Log(ref(t), args)
	if err != nil {
		return pluginErr(t.Plugin, err).task(t.ID)
	}
	if !r.OK {
		msg := "plugin rejected the entry"
		if r.Reason != nil {
			msg = *r.Reason
		}
		return fail("log_rejected", msg).task(t.ID).hint("run: tlg " + t.ID + " guide")
	}
	return a.out(map[string]any{"task_id": t.ID, "entry": r.Entry})
}

type guideResult struct {
	Scope  string  `json:"scope"`
	TaskID *string `json:"task_id"`
	plugin.Guide
}

func (a *App) taskGuide(t registry.Task, args []string) error {
	if len(args) > 1 {
		return usage("usage: tlg <id> guide [<topic>]")
	}
	c, h, e := a.plugin(t.Plugin)
	if e != nil {
		return e.task(t.ID)
	}
	if e := requireVerb(t.Plugin, h, "guide"); e != nil {
		return e.task(t.ID)
	}
	a.touch(t.ID)
	s, err := c.Status(ref(t))
	if err != nil {
		return pluginErr(t.Plugin, err).task(t.ID)
	}
	in := plugin.GuideInput{Status: s}
	if len(args) == 1 {
		in.Topic = &args[0]
	}
	g, err := c.Guide(ref(t), in, args)
	if err != nil {
		return pluginErr(t.Plugin, err).task(t.ID)
	}
	id := t.ID
	return a.out(guideResult{Scope: "plugin", TaskID: &id, Guide: g})
}

func (a *App) sessions(t registry.Task, args []string) error {
	if err := noArgs(args, "tlg <id> sessions"); err != nil {
		return err
	}
	s, err := a.db.Sessions(t.ID, 0)
	if err != nil {
		return internal(err)
	}
	return a.out(map[string]any{"task_id": t.ID, "sessions": s})
}

func (a *App) attach(t registry.Task, args []string) error {
	p, err := parseArgs(args, map[string]bool{"cwd": true, "title": true})
	if err != nil {
		return err
	}
	if len(p.pos) != 2 {
		return usage("usage: tlg <id> attach <agent> <session id> [--cwd <dir>] [--title <text>]")
	}
	cwd, ok := p.get("cwd")
	if !ok {
		if cwd, err = a.Getwd(); err != nil {
			return internal(err)
		}
	}
	title, _ := p.get("title")
	s, err := a.db.TouchSession(t.ID, p.pos[0], p.pos[1], cwd, title, a.Now())
	if err != nil {
		return internal(err)
	}
	return a.out(map[string]any{"task": t, "session": s})
}

func (a *App) parent(t registry.Task, args []string) error {
	p, err := parseArgs(args, map[string]bool{"none": false})
	if err != nil {
		return err
	}
	var parent *string
	switch {
	case p.has("none") && len(p.pos) == 0:
	case !p.has("none") && len(p.pos) == 1:
		parent = &p.pos[0]
	default:
		return usage("usage: tlg <id> parent <parent id> | tlg <id> parent --none")
	}
	err = a.db.SetParent(t.ID, parent)
	switch {
	case errors.Is(err, registry.ErrCycle):
		return fail("cycle", fmt.Sprintf("%s is already an ancestor of %s", t.ID, *parent)).task(t.ID)
	case errors.Is(err, registry.ErrNotFound):
		return fail("task_not_found", fmt.Sprintf("task %s is not in the registry", *parent)).task(*parent)
	case err != nil:
		return internal(err)
	}
	t.ParentID = parent
	warnings := []string{}
	if parent != nil {
		if pt, err := a.db.GetTask(*parent); err == nil && pt.Registry == registry.Archived {
			warnings = append(warnings, "parent "+pt.ID+" is archived")
		}
	}
	return a.out(taskResult{Task: t, Warnings: warnings})
}

func (a *App) archive(t registry.Task, args []string) error {
	if err := noArgs(args, "tlg <id> archive"); err != nil {
		return err
	}
	warnings := []string{}
	if c, _, e := a.plugin(t.Plugin); e != nil {
		warnings = append(warnings, "could not read status: "+e.Message)
	} else if s, err := c.Status(ref(t)); err != nil {
		warnings = append(warnings, "could not read status: "+pluginErr(t.Plugin, err).Message)
	} else if s.State != "closed" {
		warnings = append(warnings, "task is "+s.State+", not closed")
	}
	if err := a.db.SetRegistry(t.ID, registry.Archived); err != nil {
		return internal(err)
	}
	t.Registry = registry.Archived
	return a.out(taskResult{Task: t, Warnings: warnings})
}

func (a *App) setRegistry(t registry.Task, args []string, state, verb string) error {
	if err := noArgs(args, "tlg <id> "+verb); err != nil {
		return err
	}
	if err := a.db.SetRegistry(t.ID, state); err != nil {
		return internal(err)
	}
	t.Registry = state
	return a.out(taskResult{Task: t, Warnings: []string{}})
}

func (a *App) forget(t registry.Task, args []string) error {
	if err := noArgs(args, "tlg <id> forget"); err != nil {
		return err
	}
	n, err := a.db.Forget(t.ID)
	if err != nil {
		return internal(err)
	}
	return a.out(map[string]any{"id": t.ID, "sessions_removed": n})
}

func (a *App) passthrough(t registry.Task, verb string, args []string) error {
	c, h, e := a.plugin(t.Plugin)
	if e != nil {
		return e.task(t.ID)
	}
	if e := requireVerb(t.Plugin, h, "run"); e != nil {
		return fail("verb_unsupported", fmt.Sprintf("%q is not a tlg command and plugin %q has no pass-through verbs", verb, t.Plugin)).
			task(t.ID).hint("run: tlg help")
	}
	a.touch(t.ID)
	code, err := c.Run(ref(t), append([]string{verb}, args...))
	if err != nil {
		return pluginErr(t.Plugin, err).task(t.ID)
	}
	if code != 0 {
		return exitCode(code)
	}
	return nil
}

func joinWords(ws []string) string { return strings.Join(ws, " ") }
