// Command promptfix builds structured prompts step by step.
package main

import (
	"fmt"
	"os"

	"github.com/x0ptr/promptfix/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "promptfix:", err)
		os.Exit(1)
	}
}
