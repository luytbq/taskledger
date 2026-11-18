// Package config loads and saves the taskledger TOML configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const DefaultTimeout = 15 * time.Second

type Registry struct {
	Path string `toml:"path"`
}

type Plugin struct {
	Command string            `toml:"command"`
	Env     map[string]string `toml:"env,omitempty"`
	Timeout string            `toml:"timeout,omitempty"`
}

type Config struct {
	Registry Registry          `toml:"registry"`
	Plugins  map[string]Plugin `toml:"plugins"`
}

var pluginName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Path returns the config file location. TLG_CONFIG lets a trial run use its own
// config and registry without touching the real ones.
func Path() string {
	if p := os.Getenv("TLG_CONFIG"); p != "" {
		return Expand(p)
	}
	return Expand("~/.config/taskledger/config.toml")
}

func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// Load reads the config. A missing file is not an error: taskledger starts with
// no plugins and the default registry path.
func Load(path string) (*Config, error) {
	c := &Config{Plugins: map[string]Plugin{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c.Registry.Path = "~/.local/share/taskledger/registry.db"
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := Parse(data, c); err != nil {
		return nil, err
	}
	return c, nil
}

func Parse(data []byte, c *Config) error {
	md, err := toml.Decode(string(data), c)
	if err != nil {
		return err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return fmt.Errorf("unknown config key %q", undecoded[0].String())
	}
	if c.Plugins == nil {
		c.Plugins = map[string]Plugin{}
	}
	if c.Registry.Path == "" {
		c.Registry.Path = "~/.local/share/taskledger/registry.db"
	}
	for name, p := range c.Plugins {
		if !pluginName.MatchString(name) {
			return fmt.Errorf("plugin name %q: use lowercase letters, digits and dashes", name)
		}
		if p.Command == "" {
			return fmt.Errorf("plugin %q: command is empty", name)
		}
		if _, err := p.TimeoutDuration(); err != nil {
			return fmt.Errorf("plugin %q: %v", name, err)
		}
	}
	return nil
}

func ValidPluginName(name string) bool { return pluginName.MatchString(name) }

func (p Plugin) TimeoutDuration() (time.Duration, error) {
	if p.Timeout == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("timeout %q is not a positive duration like 15s", p.Timeout)
	}
	return d, nil
}

// ExpandedEnv returns the plugin environment with ~ expanded in values, in a
// stable order so test output does not depend on map iteration.
func (p Plugin) ExpandedEnv() []string {
	keys := make([]string, 0, len(p.Env))
	for k := range p.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+Expand(p.Env[k]))
	}
	return out
}

func Encode(c *Config) ([]byte, error) {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Save writes the config atomically: a reader never sees a half-written file.
func Save(path string, c *Config) error {
	data, err := Encode(c)
	if err != nil {
		return err
	}
	return WriteAtomic(path, data)
}

func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
