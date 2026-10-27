//go:build !windows

package agent

import (
	"os"
	"syscall"
)

// Exec replaces the current process with args, started in dir.
func Exec(args []string, dir string) error {
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			return err
		}
	}
	return syscall.Exec(args[0], args, os.Environ())
}
