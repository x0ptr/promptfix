package agent

import (
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/x0ptr/promptfix/internal/fsutil"
)

const stateFile = "launch.yaml"

// LoadLast returns the last launch options stored in dataDir, or defaults.
func LoadLast(dataDir string) Options {
	o := Options{Agent: Claude, Mode: Handoff}
	data, err := os.ReadFile(filepath.Join(dataDir, stateFile))
	if err != nil {
		return o
	}
	var saved Options
	if yaml.Unmarshal(data, &saved) != nil || saved.Validate() != nil {
		return o
	}
	return saved
}

// SaveLast stores the launch options in dataDir.
func SaveLast(dataDir string, o Options) error {
	data, err := yaml.Marshal(o)
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(filepath.Join(dataDir, stateFile), data, 0o644)
}
