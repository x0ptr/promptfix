// Package schema defines prompt schemas and loads the YAML configuration.
package schema

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"go.yaml.in/yaml/v3"
)

//go:embed default.yaml
var defaultConfig []byte

// DefaultConfigYAML returns the built-in configuration file content.
func DefaultConfigYAML() []byte { return defaultConfig }

// Section describes one tag of a schema.
type Section struct {
	Tag      string `yaml:"tag"`
	Title    string `yaml:"title"`
	Help     string `yaml:"help"`
	Example  string `yaml:"example"`
	Required bool   `yaml:"required"`

	line int
}

var sectionFields = map[string]bool{"tag": true, "title": true, "help": true, "example": true, "required": true}

// UnmarshalYAML rejects unknown fields and records the line for error messages.
func (s *Section) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.MappingNode {
		for i := 0; i < len(n.Content); i += 2 {
			if k := n.Content[i]; !sectionFields[k.Value] {
				return fmt.Errorf("line %d: unknown section field %q", k.Line, k.Value)
			}
		}
	}
	type plain Section
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	*s = Section(p)
	s.line = n.Line
	return nil
}

// DisplayTitle returns the title, or the tag if no title is set.
func (s Section) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Tag
}

// Schema is a named, ordered list of sections.
type Schema struct {
	Name     string    `yaml:"name"`
	Sections []Section `yaml:"sections"`
}

// Section returns the section with the given tag.
func (s Schema) Section(tag string) (Section, bool) {
	for _, sec := range s.Sections {
		if sec.Tag == tag {
			return sec, true
		}
	}
	return Section{}, false
}

// History configures version retention.
type History struct {
	Keep int `yaml:"keep"`
}

// Config is the user configuration merged over the built-in defaults.
type Config struct {
	DefaultSchema string   `yaml:"default_schema"`
	Editor        string   `yaml:"editor"`
	History       History  `yaml:"history"`
	Schemas       []Schema `yaml:"schemas"`
}

// Schema returns the schema with the given name; "" selects the default.
func (c *Config) Schema(name string) (Schema, error) {
	if name == "" {
		name = c.DefaultSchema
	}
	for _, s := range c.Schemas {
		if s.Name == name {
			return s, nil
		}
	}
	return Schema{}, fmt.Errorf("unknown schema %q", name)
}

// SchemaNames returns all schema names in config order.
func (c *Config) SchemaNames() []string {
	names := make([]string, len(c.Schemas))
	for i, s := range c.Schemas {
		names[i] = s.Name
	}
	return names
}

// Default returns the built-in configuration.
func Default() *Config {
	c, err := parse(defaultConfig)
	if err != nil {
		panic("built-in config: " + err.Error())
	}
	return c
}

// Load reads the config file at path and merges it over the defaults.
// A missing file yields the defaults.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	user, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.merge(user)
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func parse(data []byte) (*Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return &c, nil
}

// merge applies user settings; schemas with an existing name replace it.
func (c *Config) merge(u *Config) {
	if u.DefaultSchema != "" {
		c.DefaultSchema = u.DefaultSchema
	}
	if u.Editor != "" {
		c.Editor = u.Editor
	}
	if u.History.Keep != 0 {
		c.History.Keep = u.History.Keep
	}
	for _, us := range u.Schemas {
		replaced := false
		for i := range c.Schemas {
			if c.Schemas[i].Name == us.Name {
				c.Schemas[i] = us
				replaced = true
			}
		}
		if !replaced {
			c.Schemas = append(c.Schemas, us)
		}
	}
}

var tagPattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]*$`)

func (c *Config) validate() error {
	var errs []error
	if c.History.Keep < 0 {
		errs = append(errs, errors.New("history.keep must be >= 0"))
	}
	seen := map[string]bool{}
	for _, s := range c.Schemas {
		if s.Name == "" {
			errs = append(errs, errors.New("schema without name"))
			continue
		}
		if seen[s.Name] {
			errs = append(errs, fmt.Errorf("duplicate schema %q", s.Name))
		}
		seen[s.Name] = true
		if len(s.Sections) == 0 {
			errs = append(errs, fmt.Errorf("schema %q has no sections", s.Name))
		}
		tags := map[string]bool{}
		for _, sec := range s.Sections {
			switch {
			case !tagPattern.MatchString(sec.Tag):
				errs = append(errs, fmt.Errorf("line %d: schema %q: invalid tag %q (must match %s)", sec.line, s.Name, sec.Tag, tagPattern))
			case tags[sec.Tag]:
				errs = append(errs, fmt.Errorf("line %d: schema %q: duplicate tag %q", sec.line, s.Name, sec.Tag))
			}
			tags[sec.Tag] = true
		}
	}
	if !seen[c.DefaultSchema] {
		errs = append(errs, fmt.Errorf("default_schema %q does not exist", c.DefaultSchema))
	}
	return errors.Join(errs...)
}
