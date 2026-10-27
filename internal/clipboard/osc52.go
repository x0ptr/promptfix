// Package clipboard copies text via the OSC 52 terminal escape sequence.
// No external tools are needed; the terminal must support OSC 52.
package clipboard

import (
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

// ErrNoTerminal is returned when stdout is not a terminal.
var ErrNoTerminal = errors.New("stdout is not a terminal; OSC 52 needs one (use `show` to pipe instead)")

// Sequence returns the OSC 52 sequence that sets the clipboard to s. Inside
// tmux the sequence is wrapped for passthrough.
func Sequence(s string, inTmux bool) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(s)) + "\a"
	if inTmux {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

// Copy writes the OSC 52 sequence for s to stdout.
func Copy(s string) error {
	if !term.IsTerminal(os.Stdout.Fd()) {
		return ErrNoTerminal
	}
	return write(os.Stdout, s)
}

func write(w io.Writer, s string) error {
	_, err := io.WriteString(w, Sequence(s, os.Getenv("TMUX") != ""))
	return err
}
