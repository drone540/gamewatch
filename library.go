package main

import (
	"os"
	"sort"
	"strings"
)

// pathInside reports whether child is the same as, or below, parent. Both
// Windows and Unix separators are accepted so the helper is easy to test.
func pathInside(child, parent string) bool {
	child = cleanComparePath(child)
	parent = cleanComparePath(parent)
	if child == "" || parent == "" {
		return false
	}
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+`\`)
}

func cleanComparePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"`)
	path = strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(path, `\\?\UNC\`) {
		path = `\\` + path[len(`\\?\UNC\`):]
	} else if strings.HasPrefix(path, `\\?\`) {
		path = path[len(`\\?\`):]
	}
	path = strings.TrimRight(path, `\`)
	return strings.ToLower(path)
}

func dedupeLibraries(games []LibraryGame) []LibraryGame {
	seen := make(map[string]struct{}, len(games))
	out := make([]LibraryGame, 0, len(games))
	for _, game := range games {
		if game.Name == "" || game.InstallDir == "" {
			continue
		}
		key := strings.ToLower(strings.Join([]string{
			game.Source, game.AppID, game.Name, cleanComparePath(game.InstallDir),
		}, "|"))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, game)
	}

	// Deepest install directory first. A game installed inside another game's
	// folder then wins over the outer folder.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := len(cleanComparePath(out[i].InstallDir)), len(cleanComparePath(out[j].InstallDir))
		if a != b {
			return a > b
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
