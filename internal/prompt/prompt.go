// Package prompt holds the prompt data model and renders it to the tag format.
package prompt

import (
	"errors"
	"strings"
	"time"
)

// FormatVersion is the current on-disk file format version.
const FormatVersion = 1

// Section is one tagged block of a prompt.
type Section struct {
	Tag     string `yaml:"tag"`
	Content string `yaml:"content"`
}

// Prompt is a self-contained prompt: its section order is the render order.
type Prompt struct {
	Version  int       `yaml:"version"`
	Name     string    `yaml:"name"`
	Schema   string    `yaml:"schema"`
	Created  time.Time `yaml:"created"`
	Updated  time.Time `yaml:"updated"`
	Sections []Section `yaml:"sections"`
}

// Slug returns the file-system safe identifier of the prompt.
func (p *Prompt) Slug() string { return Slug(p.Name) }

// Filled returns the number of sections with non-blank content.
func (p *Prompt) Filled() int {
	n := 0
	for _, s := range p.Sections {
		if !IsBlank(s.Content) {
			n++
		}
	}
	return n
}

// Index returns the position of the section with the given tag, or -1.
func (p *Prompt) Index(tag string) int {
	for i, s := range p.Sections {
		if s.Tag == tag {
			return i
		}
	}
	return -1
}

// SameContent reports whether both prompts have the same name and sections.
// Timestamps and the schema reference are ignored.
func (p *Prompt) SameContent(o *Prompt) bool {
	if p.Name != o.Name || len(p.Sections) != len(o.Sections) {
		return false
	}
	for i := range p.Sections {
		if p.Sections[i] != o.Sections[i] {
			return false
		}
	}
	return true
}

// Render returns the prompt in tag format. Content is emitted verbatim,
// only trailing whitespace is trimmed; blank sections become <tag></tag>.
func Render(p *Prompt) string {
	var b strings.Builder
	for i, s := range p.Sections {
		if i > 0 {
			b.WriteString("\n")
		}
		content := strings.TrimRight(s.Content, " \t\r\n")
		if IsBlank(content) {
			b.WriteString("<" + s.Tag + "></" + s.Tag + ">\n")
			continue
		}
		b.WriteString("<" + s.Tag + ">\n" + content + "\n</" + s.Tag + ">\n")
	}
	return b.String()
}

// IsBlank reports whether s contains only whitespace.
func IsBlank(s string) bool { return strings.TrimSpace(s) == "" }

// ErrInvalidName is returned when a name yields an empty slug.
var ErrInvalidName = errors.New("name must contain at least one letter or digit")

var transliterate = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")

// Slug lowercases name, transliterates German umlauts and replaces every run
// of characters outside [a-z0-9] with a single dash.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range transliterate.Replace(strings.ToLower(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// ValidateName checks that name can be used as a prompt name.
func ValidateName(name string) error {
	if Slug(name) == "" {
		return ErrInvalidName
	}
	return nil
}
