//go:build windows

package agent

import (
	"errors"
	"os"
	"os/exec"
)

// Exec runs args in dir with the terminal attached and exits with its exit
// code; Windows has no exec that replaces the process.
func Exec(args []string, dir string) error {
	if err := CheckLength(args); err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	if err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
