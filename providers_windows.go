//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// scanLibraries discovers installed games from launchers. Returning install
// directories (instead of walking every game folder for executables) keeps a
// scan fast even with hundreds of installed games.
func scanLibraries(cfg Config, verbose bool) ([]LibraryGame, []string) {
	var games []LibraryGame
	var warnings []string

	if cfg.Steam {
		roots := steamRoots()
		found := collectSteamGames(roots, &warnings)
		if verbose && len(roots) == 0 {
			warnings = append(warnings, "steam: installation not found")
		}
		games = append(games, found...)
	}
	if cfg.Epic {
		games = append(games, scanEpic(&warnings)...)
	}
	if cfg.GOG {
		games = append(games, scanGOG(&warnings)...)
	}
	if cfg.Ubisoft {
		games = append(games, scanUbisoft(&warnings)...)
	}
	return dedupeLibraries(games), warnings
}

func steamRoots() []string {
	var roots []string
	seen := make(map[string]struct{})
	add := func(path string) {
		path = cleanRegistryPath(path)
		if path == "" {
			return
		}
		key := cleanComparePath(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		roots = append(roots, path)
	}

	if key, err := openRegistryKey(registry.CURRENT_USER, `Software\Valve\Steam`); err == nil {
		if value, _, err := key.GetStringValue("SteamPath"); err == nil {
			add(value)
		}
		_ = key.Close()
	}
	for _, keyPath := range []string{
		`SOFTWARE\WOW6432Node\Valve\Steam`,
		`SOFTWARE\Valve\Steam`,
	} {
		if key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE); err == nil {
			if value, _, err := key.GetStringValue("InstallPath"); err == nil {
				add(value)
			}
			_ = key.Close()
		}
	}
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
		if dir := os.Getenv(env); dir != "" {
			add(filepath.Join(dir, "Steam"))
		}
	}
	return roots
}

func scanEpic(warnings *[]string) []LibraryGame {
	dirs := epicManifestDirs()
	var games []LibraryGame
	seenDirs := make(map[string]struct{})

	for _, dir := range dirs {
		dir = filepath.Clean(dir)
		key := cleanComparePath(dir)
		if _, ok := seenDirs[key]; ok {
			continue
		}
		seenDirs[key] = struct{}{}

		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".item") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				*warnings = append(*warnings, "epic: read "+path+": "+err.Error())
				continue
			}
			var manifest epicManifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				*warnings = append(*warnings, "epic: parse "+path+": "+err.Error())
				continue
			}
			if manifest.Incomplete {
				continue
			}
			// DLC manifests usually point at the same install directory as
			// the base game. Skip them so the base game keeps its name.
			if manifest.MainGameAppName != "" && !strings.EqualFold(manifest.MainGameAppName, manifest.AppName) {
				continue
			}
			name := manifest.DisplayName
			if name == "" {
				name = manifest.AppName
			}
			installDir := cleanRegistryPath(manifest.InstallLocation)
			if name == "" || installDir == "" || !isDir(installDir) {
				continue
			}
			if strings.HasPrefix(strings.ToLower(name), "unreal engine") {
				continue
			}
			appID := manifest.CatalogItemId
			if appID == "" {
				appID = manifest.AppName
			}
			exe := launchExeName(manifest.LaunchExecutable)
			games = append(games, LibraryGame{
				Name:       name,
				Source:     "epic",
				AppID:      appID,
				InstallDir: installDir,
				LaunchExe:  exe,
			})
		}
	}

	// LauncherInstalled.dat is a smaller fallback used by some Epic installs.
	for _, path := range []string{
		`C:\ProgramData\Epic\UnrealEngineLauncher\LauncherInstalled.dat`,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			InstallationList []struct {
				AppName          string `json:"AppName"`
				DisplayName      string `json:"DisplayName"`
				InstallLocation  string `json:"InstallLocation"`
				LaunchExecutable string `json:"LaunchExecutable"`
			} `json:"InstallationList"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			*warnings = append(*warnings, "epic: parse "+path+": "+err.Error())
			continue
		}
		for _, item := range doc.InstallationList {
			name := item.DisplayName
			if name == "" {
				name = item.AppName
			}
			installDir := cleanRegistryPath(item.InstallLocation)
			if name == "" || installDir == "" || !isDir(installDir) {
				continue
			}
			if strings.HasPrefix(strings.ToLower(name), "unreal engine") {
				continue
			}
			games = append(games, LibraryGame{
				Name:       name,
				Source:     "epic",
				AppID:      item.AppName,
				InstallDir: installDir,
				LaunchExe:  launchExeName(item.LaunchExecutable),
			})
		}
	}
	return games
}

type epicManifest struct {
	DisplayName      string `json:"DisplayName"`
	InstallLocation  string `json:"InstallLocation"`
	LaunchExecutable string `json:"LaunchExecutable"`
	AppName          string `json:"AppName"`
	MainGameAppName  string `json:"MainGameAppName"`
	CatalogItemId    string `json:"CatalogItemId"`
	Incomplete       bool   `json:"bIsIncompleteInstall"`
}

func epicManifestDirs() []string {
	var dirs []string
	for _, keyPath := range []string{
		`SOFTWARE\WOW6432Node\Epic Games\EpicGamesLauncher`,
		`SOFTWARE\Epic Games\EpicGamesLauncher`,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, keyPath, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		if appData, _, err := key.GetStringValue("AppDataPath"); err == nil && appData != "" {
			appData = cleanRegistryPath(appData)
			if strings.EqualFold(filepath.Base(appData), "Manifests") {
				dirs = append(dirs, appData)
			} else {
				dirs = append(dirs, filepath.Join(appData, "Manifests"))
			}
		}
		_ = key.Close()
	}
	dirs = append(dirs, `C:\ProgramData\Epic\EpicGamesLauncher\Data\Manifests`)
	return dirs
}

func scanGOG(warnings *[]string) []LibraryGame {
	var games []LibraryGame
	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		for _, keyPath := range []string{
			`SOFTWARE\WOW6432Node\GOG.com\Games`,
			`SOFTWARE\GOG.com\Games`,
		} {
			key, err := registry.OpenKey(root, keyPath, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			ids, err := key.ReadSubKeyNames(-1)
			if err != nil {
				_ = key.Close()
				continue
			}
			for _, id := range ids {
				sub, err := registry.OpenKey(key, id, registry.QUERY_VALUE)
				if err != nil {
					continue
				}
				name, _, _ := sub.GetStringValue("gameName")
				installDir, _, _ := sub.GetStringValue("path")
				exe, _, _ := sub.GetStringValue("exe")
				_ = sub.Close()

				installDir = cleanRegistryPath(installDir)
				if installDir == "" && exe != "" {
					installDir = filepath.Dir(cleanRegistryPath(exe))
				}
				if name == "" || installDir == "" || !isDir(installDir) {
					continue
				}
				launchExe := launchExeName(exe)
				games = append(games, LibraryGame{
					Name:       name,
					Source:     "gog",
					AppID:      id,
					InstallDir: installDir,
					LaunchExe:  launchExe,
				})
			}
			_ = key.Close()
		}
	}
	return games
}

func scanUbisoft(warnings *[]string) []LibraryGame {
	var games []LibraryGame
	candidates := []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Ubisoft\Launcher\Installs`},
		{registry.LOCAL_MACHINE, `SOFTWARE\Ubisoft\Launcher\Installs`},
		{registry.CURRENT_USER, `SOFTWARE\Ubisoft\Launcher\Installs`},
		{registry.CURRENT_USER, `SOFTWARE\WOW6432Node\Ubisoft\Launcher\Installs`},
	}
	for _, candidate := range candidates {
		key, err := registry.OpenKey(candidate.root, candidate.path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		ids, err := key.ReadSubKeyNames(-1)
		if err != nil {
			_ = key.Close()
			continue
		}
		for _, id := range ids {
			sub, err := registry.OpenKey(key, id, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name, _, _ := sub.GetStringValue("DisplayName")
			installDir, _, _ := sub.GetStringValue("InstallDir")
			_ = sub.Close()

			installDir = cleanRegistryPath(installDir)
			if installDir == "" || !isDir(installDir) {
				continue
			}
			if name == "" {
				name = filepath.Base(installDir)
			}
			lowerName := strings.ToLower(name)
			if lowerName == "ubisoft connect" || lowerName == "uplay" {
				continue
			}
			games = append(games, LibraryGame{
				Name:       name,
				Source:     "ubisoft",
				AppID:      id,
				InstallDir: installDir,
			})
		}
		_ = key.Close()
	}
	return games
}

func openRegistryKey(root registry.Key, path string) (registry.Key, error) {
	key, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err == nil {
		return key, nil
	}
	return registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_32KEY)
}

func steamRunningAppID() string {
	key, err := openRegistryKey(registry.CURRENT_USER, `Software\Valve\Steam`)
	if err != nil {
		return ""
	}
	defer key.Close()

	if value, _, err := key.GetIntegerValue("RunningAppID"); err == nil {
		if value == 0 {
			return ""
		}
		return strconv.FormatUint(value, 10)
	}
	if value, _, err := key.GetStringValue("RunningAppID"); err == nil {
		value = strings.TrimSpace(value)
		if value != "" && value != "0" {
			return value
		}
	}
	return ""
}

func cleanRegistryPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"`)
	if path == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(path))
}

func launchExeName(path string) string {
	path = cleanRegistryPath(path)
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}
