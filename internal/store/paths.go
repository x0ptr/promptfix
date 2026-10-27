package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

const appName = "promptfix"

// DataDir returns the directory holding prompts and history.
//
// Order: $PROMPTFIX_HOME, then %LOCALAPPDATA% on Windows,
// $XDG_DATA_HOME or $HOME/.local/share elsewhere (including macOS).
func DataDir() (string, error) {
	if d := os.Getenv("PROMPTFIX_HOME"); d != "" {
		return d, nil
	}
	if runtime.GOOS == "windows" {
		return windowsDir("LOCALAPPDATA")
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", appName), nil
}

// ConfigPath returns the path of the config file.
//
// Order: $PROMPTFIX_CONFIG, then %APPDATA% on Windows,
// $XDG_CONFIG_HOME or $HOME/.config elsewhere (including macOS).
func ConfigPath() (string, error) {
	if p := os.Getenv("PROMPTFIX_CONFIG"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		d, err := windowsDir("APPDATA")
		if err != nil {
			return "", err
		}
		return filepath.Join(d, "config.yaml"), nil
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, appName, "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", appName, "config.yaml"), nil
}

func windowsDir(env string) (string, error) {
	d := os.Getenv(env)
	if d == "" {
		return "", errors.New("%" + env + "% is not set")
	}
	return filepath.Join(d, appName), nil
}
