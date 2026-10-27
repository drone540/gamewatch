package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const steamRedistributablesAppID = "228980"

// steamStateFullyInstalled is Steam's StateFlags bit for a completely
// installed app. A manifest without StateFlags is treated as installed.
const steamStateFullyInstalled = 4

// collectSteamGames reads appmanifest_*.acf files from the root and every
// library listed in libraryfolders.vdf. It never walks game folders.
func collectSteamGames(roots []string, warnings *[]string) []LibraryGame {
	var games []LibraryGame
	seenRoots := make(map[string]struct{})

	for _, root := range roots {
		root = filepath.Clean(root)
		if root == "" || root == "." || !isDir(filepath.Join(root, "steamapps")) {
			continue
		}
		rootKey := cleanComparePath(root)
		if _, ok := seenRoots[rootKey]; ok {
			continue
		}
		seenRoots[rootKey] = struct{}{}

		libraries := []string{root}
		libraryFile := filepath.Join(root, "steamapps", "libraryfolders.vdf")
		if data, err := os.ReadFile(libraryFile); err == nil {
			doc, err := parseVDF(string(data))
			if err != nil {
				*warnings = append(*warnings, "steam: parse "+libraryFile+": "+err.Error())
			} else {
				folders := vdfObject(doc, "libraryfolders")
				if folders == nil {
					folders = doc
				}
				for key, value := range folders {
					switch typed := value.(type) {
					case string:
						// Old-format libraryfolders.vdf stores paths under
						// numeric keys. Ignore metadata such as ContentStatsID.
						if _, err := strconv.Atoi(key); err == nil {
							libraries = append(libraries, typed)
						}
					case map[string]any:
						if path := vdfString(typed, "path"); path != "" {
							libraries = append(libraries, path)
						}
					}
				}
			}
		}

		seenLibs := make(map[string]struct{})
		for _, library := range libraries {
			library = filepath.Clean(strings.TrimSpace(library))
			if library == "" {
				continue
			}
			libKey := cleanComparePath(library)
			if _, ok := seenLibs[libKey]; ok {
				continue
			}
			seenLibs[libKey] = struct{}{}

			steamapps := filepath.Join(library, "steamapps")
			entries, err := os.ReadDir(steamapps)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				lower := strings.ToLower(name)
				if !strings.HasPrefix(lower, "appmanifest_") || !strings.HasSuffix(lower, ".acf") {
					continue
				}
				appID := name[len("appmanifest_") : len(name)-len(".acf")]
				if appID == "" || appID == steamRedistributablesAppID {
					continue
				}

				manifestPath := filepath.Join(steamapps, name)
				data, err := os.ReadFile(manifestPath)
				if err != nil {
					*warnings = append(*warnings, "steam: read "+manifestPath+": "+err.Error())
					continue
				}
				doc, err := parseVDF(string(data))
				if err != nil {
					*warnings = append(*warnings, "steam: parse "+manifestPath+": "+err.Error())
					continue
				}
				state := vdfObject(doc, "AppState")
				if state == nil {
					continue
				}
				if flagsText := vdfString(state, "StateFlags"); flagsText != "" {
					if flags, err := strconv.ParseUint(strings.TrimSpace(flagsText), 10, 64); err == nil {
						if flags&steamStateFullyInstalled == 0 {
							continue
						}
					}
				}

				installDir := vdfString(state, "installdir")
				if installDir == "" || filepath.IsAbs(installDir) ||
					strings.Contains(installDir, "..") {
					continue
				}
				installPath := filepath.Join(steamapps, "common", installDir)
				if !isDir(installPath) {
					continue
				}
				gameName := vdfString(state, "name")
				if gameName == "" {
					gameName = installDir
				}
				games = append(games, LibraryGame{
					Name:       gameName,
					Source:     "steam",
					AppID:      appID,
					InstallDir: installPath,
				})
			}
		}
	}
	return games
}
