package rules

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// File is a parsed rule file. It keeps the original bytes so that edits
// change only the parts that were actually edited.
type File struct {
	raw       []byte
	nl        string // line ending used by the file
	hasFM     bool
	fmLines   []string // frontmatter lines between the delimiters, with endings
	open      string   // opening delimiter line incl. ending
	close     string   // closing delimiter line incl. ending
	bodyStart int
	paths     []string
	pathsAt   [2]int // [start, end) line indices of the paths block; start < 0 if absent
	readOnly  string
}

// Parse parses a rule file. It never fails: files it cannot handle safely
// are marked read-only (see ReadOnly).
func Parse(data []byte) *File {
	f := &File{raw: data, nl: "\n", pathsAt: [2]int{-1, -1}}
	// The first line ending decides; mixed files are common after edits.
	if i := bytes.IndexByte(data, '\n'); i > 0 && data[i-1] == '\r' {
		f.nl = "\r\n"
	}
	lines := splitLines(string(data))
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r\n") != "---" {
		return f
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		f.readOnly = "frontmatter is not closed with ---"
		return f
	}
	f.hasFM = true
	f.open, f.close = lines[0], lines[end]
	f.fmLines = lines[1:end]
	for _, l := range lines[:end+1] {
		f.bodyStart += len(l)
	}

	var meta map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(f.fmLines, "")), &meta); err != nil {
		f.readOnly = "invalid frontmatter: " + err.Error()
		return f
	}
	v, ok := meta["paths"]
	if !ok {
		return f
	}
	switch v := v.(type) {
	case nil:
	case string:
		f.paths = []string{v}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				f.readOnly = "paths must be a list of strings"
				return f
			}
			f.paths = append(f.paths, s)
		}
	default:
		f.readOnly = "paths must be a string or a list of strings"
		return f
	}
	f.pathsAt = findBlock(f.fmLines, "paths")
	if f.pathsAt[0] < 0 {
		f.readOnly = "paths key is not in a supported format"
	}
	return f
}

// findBlock returns the line range of a top-level key and its continuation
// lines (indented lines and block sequence items).
func findBlock(lines []string, key string) [2]int {
	for i, l := range lines {
		if !strings.HasPrefix(l, key+":") {
			continue
		}
		j := i + 1
		for j < len(lines) && (strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t") || strings.HasPrefix(lines[j], "-")) {
			j++
		}
		return [2]int{i, j}
	}
	return [2]int{-1, -1}
}

// splitLines splits s into lines, keeping the line endings.
func splitLines(s string) []string {
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Raw returns the file content.
func (f *File) Raw() []byte { return f.raw }

// Body returns the Markdown body after the frontmatter.
func (f *File) Body() string { return string(f.raw[f.bodyStart:]) }

// Paths returns the globs of the paths key; nil means the rule always applies.
func (f *File) Paths() []string { return f.paths }

// ReadOnly returns why the file cannot be edited field by field, or "".
func (f *File) ReadOnly() string { return f.readOnly }

// LineEnding returns the line ending used by the file.
func (f *File) LineEnding() string { return f.nl }

// With returns the file content with the given paths and body. Parts that
// did not change are kept byte for byte.
func (f *File) With(paths []string, body string) ([]byte, error) {
	if f.readOnly != "" {
		return nil, fmt.Errorf("read-only: %s", f.readOnly)
	}
	prefix := string(f.raw[:f.bodyStart])
	if !slices.Equal(paths, f.paths) {
		prefix = f.prefixWith(paths)
	}
	return []byte(prefix + body), nil
}

func (f *File) prefixWith(paths []string) string {
	var block []string
	if len(paths) > 0 {
		block = append(block, "paths:"+f.nl)
		for _, p := range paths {
			block = append(block, "  - "+quote(p)+f.nl)
		}
	}
	if !f.hasFM {
		if len(block) == 0 {
			return ""
		}
		return "---" + f.nl + strings.Join(block, "") + "---" + f.nl
	}
	lines := slices.Clone(f.fmLines)
	if f.pathsAt[0] >= 0 {
		lines = slices.Replace(lines, f.pathsAt[0], f.pathsAt[1], block...)
	} else {
		if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
			lines[n-1] += f.nl
		}
		lines = append(lines, block...)
	}
	if strings.TrimSpace(strings.Join(lines, "")) == "" {
		// Only paths was in the frontmatter; drop the empty frontmatter.
		return ""
	}
	return f.open + strings.Join(lines, "") + f.close
}

// quote returns s as a double-quoted YAML scalar.
func quote(s string) string {
	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Style: yaml.DoubleQuotedStyle, Value: s})
	if err != nil {
		return `"` + s + `"`
	}
	return strings.TrimSuffix(string(out), "\n")
}

// New returns the content of a new rule file.
func New(paths []string, body string) []byte {
	out, _ := Parse(nil).With(paths, body)
	return out
}
