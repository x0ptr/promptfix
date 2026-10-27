package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefault(t *testing.T) {
	c := Default()
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	s, err := c.Schema("")
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, sec := range s.Sections {
		tags = append(tags, sec.Tag)
	}
	want := "product glossary technology cross_cutting deviations non_goals task acceptance way_of_working"
	if got := strings.Join(tags, " "); got != want {
		t.Fatalf("default tags = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.DefaultSchema != "default" {
		t.Fatalf("DefaultSchema = %q", c.DefaultSchema)
	}
}

func TestLoadMerge(t *testing.T) {
	path := writeConfig(t, `
default_schema: short
history:
  keep: 5
schemas:
  - name: short
    sections:
      - tag: task
      - tag: done
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.History.Keep != 5 || len(c.Schemas) != 2 {
		t.Fatalf("unexpected config: %+v", c)
	}
	s, err := c.Schema("")
	if err != nil || s.Name != "short" || len(s.Sections) != 2 {
		t.Fatalf("Schema(\"\") = %+v, %v", s, err)
	}
}

func TestLoadOverrideDefault(t *testing.T) {
	c, err := Load(writeConfig(t, "schemas:\n  - name: default\n    sections:\n      - tag: only\n"))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := c.Schema("default")
	if len(s.Sections) != 1 || s.Sections[0].Tag != "only" {
		t.Fatalf("default not replaced: %+v", s)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"unknown top-level": "foo: 1\n",
		"unknown section":   "schemas:\n  - name: x\n    sections:\n      - tag: a\n        titel: A\n",
		"invalid tag":       "schemas:\n  - name: x\n    sections:\n      - tag: Bad Tag\n",
		"duplicate tag":     "schemas:\n  - name: x\n    sections:\n      - tag: a\n      - tag: a\n",
		"empty schema":      "schemas:\n  - name: x\n",
		"missing default":   "default_schema: nope\n",
		"negative keep":     "history:\n  keep: -1\n",
	}
	for name, content := range cases {
		if _, err := Load(writeConfig(t, content)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadErrorHasLine(t *testing.T) {
	_, err := Load(writeConfig(t, "schemas:\n  - name: x\n    sections:\n      - tag: ok\n      - tag: Bad\n"))
	if err == nil || !strings.Contains(err.Error(), "line 5") {
		t.Fatalf("expected line 5 in error, got %v", err)
	}
}
