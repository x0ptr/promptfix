package rules

import (
	"slices"
	"testing"
)

func mustWith(t *testing.T, f *File, paths []string, body string) string {
	t.Helper()
	out, err := f.With(paths, body)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestParseNoFrontmatter(t *testing.T) {
	f := Parse([]byte("# Rule\nDo X.\n"))
	if f.ReadOnly() != "" || f.Paths() != nil || f.Body() != "# Rule\nDo X.\n" {
		t.Fatalf("unexpected: ro=%q paths=%v body=%q", f.ReadOnly(), f.Paths(), f.Body())
	}
	got := mustWith(t, f, []string{"src/**/*.go"}, f.Body())
	want := "---\npaths:\n  - \"src/**/*.go\"\n---\n# Rule\nDo X.\n"
	if got != want {
		t.Fatalf("add paths:\n%q\nwant\n%q", got, want)
	}
	if got := mustWith(t, f, nil, "new\n"); got != "new\n" {
		t.Fatalf("body only: %q", got)
	}
}

// R3: unknown frontmatter fields, comments and order stay byte-identical.
func TestPreserveFrontmatter(t *testing.T) {
	src := "---\n# owner: team-a\ndescription:   Tenant rule  \npaths:\n- src/api/**\n- 'src/db/*.sql'   # db\nx-custom: {a: 1}\n---\nbody\n"
	f := Parse([]byte(src))
	if f.ReadOnly() != "" {
		t.Fatal(f.ReadOnly())
	}
	if !slices.Equal(f.Paths(), []string{"src/api/**", "src/db/*.sql"}) {
		t.Fatalf("paths %v", f.Paths())
	}
	// Nothing changed: identical bytes.
	if got := mustWith(t, f, f.Paths(), f.Body()); got != src {
		t.Fatalf("unchanged:\n%q", got)
	}
	// Body changed: frontmatter identical.
	want := "---\n# owner: team-a\ndescription:   Tenant rule  \npaths:\n- src/api/**\n- 'src/db/*.sql'   # db\nx-custom: {a: 1}\n---\nnew body\n"
	if got := mustWith(t, f, f.Paths(), "new body\n"); got != want {
		t.Fatalf("body change:\n%q", got)
	}
	// Paths changed: only the paths block differs.
	want = "---\n# owner: team-a\ndescription:   Tenant rule  \npaths:\n  - \"lib/**\"\nx-custom: {a: 1}\n---\nbody\n"
	if got := mustWith(t, f, []string{"lib/**"}, f.Body()); got != want {
		t.Fatalf("paths change:\n%q", got)
	}
	// Paths removed: block gone, rest kept.
	want = "---\n# owner: team-a\ndescription:   Tenant rule  \nx-custom: {a: 1}\n---\nbody\n"
	if got := mustWith(t, f, nil, f.Body()); got != want {
		t.Fatalf("paths removed:\n%q", got)
	}
}

func TestPathsOnlyFrontmatterDropped(t *testing.T) {
	f := Parse([]byte("---\npaths: \"*.md\"\n---\nbody"))
	if !slices.Equal(f.Paths(), []string{"*.md"}) {
		t.Fatalf("paths %v", f.Paths())
	}
	if got := mustWith(t, f, nil, f.Body()); got != "body" {
		t.Fatalf("got %q", got)
	}
}

func TestAddPathsToExistingFrontmatter(t *testing.T) {
	f := Parse([]byte("---\ndescription: x\n---\nbody\n"))
	want := "---\ndescription: x\npaths:\n  - \"a/*\"\n---\nbody\n"
	if got := mustWith(t, f, []string{"a/*"}, f.Body()); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestCRLF(t *testing.T) {
	f := Parse([]byte("---\r\ndescription: x\r\n---\r\nbody\r\n"))
	if f.LineEnding() != "\r\n" || f.Body() != "body\r\n" {
		t.Fatalf("nl=%q body=%q", f.LineEnding(), f.Body())
	}
	want := "---\r\ndescription: x\r\npaths:\r\n  - \"a\"\r\n---\r\nbody\r\n"
	if got := mustWith(t, f, []string{"a"}, f.Body()); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestReadOnly(t *testing.T) {
	cases := map[string]string{
		"unclosed":      "---\npaths: a\nbody",
		"invalid yaml":  "---\npaths: [a\n---\n",
		"paths map":     "---\npaths:\n  a: b\n---\n",
		"paths numbers": "---\npaths: [1, 2]\n---\n",
		"quoted key":    "---\n\"paths\": a\n---\n",
	}
	for name, src := range cases {
		f := Parse([]byte(src))
		if f.ReadOnly() == "" {
			t.Errorf("%s: expected read-only", name)
		}
		if _, err := f.With(nil, ""); err == nil {
			t.Errorf("%s: With should fail", name)
		}
	}
}

func TestNew(t *testing.T) {
	if got := string(New(nil, "x\n")); got != "x\n" {
		t.Fatalf("got %q", got)
	}
	if got := string(New([]string{`we"ird`}, "x\n")); got != "---\npaths:\n  - \"we\\\"ird\"\n---\nx\n" {
		t.Fatalf("got %q", got)
	}
}
