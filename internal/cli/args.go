package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// parsed holds a command's arguments: flags (repeatable), positionals, and
// everything after a bare "--".
type parsed struct {
	flags map[string][]string
	pos   []string
	rest  []string
}

func (p parsed) has(name string) bool { _, ok := p.flags[name]; return ok }

func (p parsed) get(name string) (string, bool) {
	v, ok := p.flags[name]
	if !ok || len(v) == 0 {
		return "", ok
	}
	return v[len(v)-1], true
}

// parseArgs accepts only the flags in spec; spec[name] says whether the flag takes
// a value. Flags may appear anywhere before "--".
func parseArgs(args []string, spec map[string]bool) (parsed, error) {
	p := parsed{flags: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			p.rest = append([]string{}, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") || a == "-" {
			p.pos = append(p.pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		takes, ok := spec[name]
		if !ok {
			return p, usage("unknown option --" + name)
		}
		switch {
		case takes && hasVal:
			p.flags[name] = append(p.flags[name], val)
		case takes:
			if i+1 >= len(args) {
				return p, usage("option --" + name + " needs a value")
			}
			i++
			p.flags[name] = append(p.flags[name], args[i])
		case hasVal:
			return p, usage("option --" + name + " takes no value")
		default:
			p.flags[name] = append(p.flags[name], "")
		}
	}
	if p.rest == nil {
		p.rest = []string{}
	}
	return p, nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// checkID enforces the task id rules: ids are typed in shells and used as file
// names, so plain ASCII only, and never a core verb because "tlg <id> <verb>"
// would become ambiguous.
func checkID(id string) *Error {
	if !idPattern.MatchString(id) {
		return fail("invalid_id", "task id "+quote(id)+" may only contain ASCII letters, digits, '_', '-', '.' and must start with a letter or digit").task(id)
	}
	if isReserved(id) {
		return fail("invalid_id", "task id "+quote(id)+" is a tlg command name").task(id)
	}
	return nil
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

var vnFold = buildFold(map[rune]string{
	'a': "àáảãạăằắẳẵặâầấẩẫậ",
	'e': "èéẻẽẹêềếểễệ",
	'i': "ìíỉĩị",
	'o': "òóỏõọôồốổỗộơờớởỡợ",
	'u': "ùúủũụưừứửữự",
	'y': "ỳýỷỹỵ",
	'd': "đ",
})

func buildFold(groups map[rune]string) map[rune]rune {
	m := map[rune]rune{}
	for base, s := range groups {
		for _, r := range s {
			m[r] = base
		}
	}
	return m
}

// slug turns a title into the short part of a default child id: Vietnamese
// letters lose their marks, anything else non-alphanumeric becomes a dash.
func slug(title string, maxLen int) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if f, ok := vnFold[r]; ok {
			r = f
		}
		if r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= maxLen {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
