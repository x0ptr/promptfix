package prompt

import "testing"

func TestRender(t *testing.T) {
	p := &Prompt{Sections: []Section{
		{Tag: "product", Content: "A tool.\n\n"},
		{Tag: "glossary", Content: "  \n"},
		{Tag: "acceptance", Content: "  indented\n<b>tag-like</b> </acceptance> text"},
	}}
	want := "<product>\nA tool.\n</product>\n" +
		"\n<glossary></glossary>\n" +
		"\n<acceptance>\n  indented\n<b>tag-like</b> </acceptance> text\n</acceptance>\n"
	if got := Render(p); got != want {
		t.Fatalf("Render mismatch\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"My API Project":  "my-api-project",
		"  --x__y--  ":    "x-y",
		"Prompt #2 (v1)!": "prompt-2-v1",
		"Prüfung Größe":   "pruefung-groesse",
		"":                "",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameContent(t *testing.T) {
	a := &Prompt{Name: "a", Sections: []Section{{Tag: "x", Content: "1"}}}
	b := &Prompt{Name: "a", Schema: "other", Sections: []Section{{Tag: "x", Content: "1"}}}
	if !a.SameContent(b) {
		t.Fatal("expected same content")
	}
	b.Sections[0].Content = "2"
	if a.SameContent(b) {
		t.Fatal("expected different content")
	}
}
