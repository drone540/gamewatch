package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CustomGame is user-configured detection. A custom game can be matched by
// executable name, install directory, window title, or a combination.
type CustomGame struct {
	Name             string   `json:"name"`
	AppID            string   `json:"appId,omitempty"`
	Executables      []string `json:"executables,omitempty"`
	InstallDir       string   `json:"installDir,omitempty"`
	MatchWindowTitle string   `json:"matchWindowTitle,omitempty"`
}

// Config controls detection and output. Defaults are applied before the JSON
// file is decoded, so omitted fields keep their default values.
type Config struct {
	PollSeconds             int          `json:"pollSeconds"`
	StartDelaySeconds       int          `json:"startDelaySeconds"`
	StopDelaySeconds        int          `json:"stopDelaySeconds"`
	LibraryRefreshMinutes   int          `json:"libraryRefreshMinutes"`
	Steam                   bool         `json:"steam"`
	Epic                    bool         `json:"epic"`
	GOG                     bool         `json:"gog"`
	Ubisoft                 bool         `json:"ubisoft"`
	DetectUnknownFullscreen bool         `json:"detectUnknownFullscreen"`
	IgnoredExecutables      []string     `json:"ignoredExecutables,omitempty"`
	IgnoredGames            []string     `json:"ignoredGames,omitempty"`
	Games                   []CustomGame `json:"games,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		PollSeconds:           5,
		StartDelaySeconds:     10,
		StopDelaySeconds:      20,
		LibraryRefreshMinutes: 60,
		Steam:                 true,
		Epic:                  true,
		GOG:                   true,
		Ubisoft:               true,
	}
}

// LoadConfig loads path, or gamewatch.json from the current directory / next
// to the executable when path is empty. A missing default config is not an
// error; built-in defaults are used instead.
func LoadConfig(path string) (Config, string, error) {
	cfg := DefaultConfig()

	explicit := path != ""
	usedPath := path
	if usedPath == "" {
		for _, candidate := range defaultConfigPaths() {
			if fileExists(candidate) {
				usedPath = candidate
				break
			}
		}
	}

	if usedPath != "" {
		data, err := os.ReadFile(usedPath)
		if err != nil {
			return cfg, "", fmt.Errorf("read config: %w", err)
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, "", fmt.Errorf("parse %s: %w", usedPath, err)
		}
	} else if explicit {
		return cfg, "", fmt.Errorf("config file %q does not exist", path)
	}

	if err := cfg.normalize(); err != nil {
		if usedPath != "" {
			return cfg, usedPath, fmt.Errorf("%s: %w", usedPath, err)
		}
		return cfg, "", err
	}
	return cfg, usedPath, nil
}

func defaultConfigPaths() []string {
	var paths []string
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(cwd, "gamewatch.json"))
	}
	if exe, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(exe), "gamewatch.json")
		if len(paths) == 0 || !samePath(path, paths[0]) {
			paths = append(paths, path)
		}
	}
	return paths
}

func (c *Config) normalize() error {
	if c.PollSeconds < 1 {
		c.PollSeconds = 5
	}
	if c.StartDelaySeconds < 0 {
		c.StartDelaySeconds = 10
	}
	if c.StopDelaySeconds < 1 {
		c.StopDelaySeconds = 1
	}
	if c.LibraryRefreshMinutes < 1 {
		c.LibraryRefreshMinutes = 60
	}

	for i := range c.Games {
		g := &c.Games[i]
		g.Name = strings.TrimSpace(g.Name)
		g.AppID = strings.TrimSpace(g.AppID)
		g.InstallDir = strings.TrimSpace(g.InstallDir)
		g.MatchWindowTitle = strings.TrimSpace(g.MatchWindowTitle)
		exes := g.Executables[:0]
		for _, exe := range g.Executables {
			exe = strings.TrimSpace(exe)
			if exe != "" {
				exes = append(exes, exe)
			}
		}
		g.Executables = exes

		if g.Name == "" {
			return fmt.Errorf("games[%d]: name is required", i)
		}
		if len(g.Executables) == 0 && g.InstallDir == "" {
			return fmt.Errorf("games[%d] (%s): set at least one executable or installDir", i, g.Name)
		}
	}
	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
