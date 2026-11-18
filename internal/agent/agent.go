// Package agent recognises which AI agent session, if any, is running tlg.
package agent

import (
	"os"
	"strings"
)

type Session struct {
	Agent string
	ID    string
}

type detector struct {
	agent    string
	matches  func(getenv func(string) string) bool
	idEnvVar string
}

// Only agents whose environment has been checked by hand belong here. Claude Code
// 2.1 sets AI_AGENT=claude-code_<version>_agent and CLAUDE_CODE_SESSION_ID in
// every shell it runs.
var detectors = []detector{
	{
		agent:    "claude",
		matches:  func(g func(string) string) bool { return strings.HasPrefix(g("AI_AGENT"), "claude-code") },
		idEnvVar: "CLAUDE_CODE_SESSION_ID",
	},
}

func Detect() (Session, bool) { return detect(os.Getenv) }

func detect(getenv func(string) string) (Session, bool) {
	for _, d := range detectors {
		if !d.matches(getenv) {
			continue
		}
		if id := getenv(d.idEnvVar); id != "" {
			return Session{Agent: d.agent, ID: id}, true
		}
	}
	return Session{}, false
}
