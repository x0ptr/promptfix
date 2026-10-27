package clipboard

import "testing"

func TestSequence(t *testing.T) {
	if got, want := Sequence("hi", false), "\x1b]52;c;aGk=\a"; got != want {
		t.Errorf("plain: got %q, want %q", got, want)
	}
	if got, want := Sequence("hi", true), "\x1bPtmux;\x1b\x1b]52;c;aGk=\a\x1b\\"; got != want {
		t.Errorf("tmux: got %q, want %q", got, want)
	}
}
