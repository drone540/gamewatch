package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

//go:embed games.json
var builtinGamesJSON []byte

type GamePreset struct {
	Name             string   `json:"name"`
	AppID            string   `json:"appId,omitempty"`
	Executables      []string `json:"executables"`
	MatchWindowTitle string   `json:"matchWindowTitle,omitempty"`
}

type Detector struct {
	cfg     Config
	verbose bool

	builtin      map[string][]GamePreset
	libraries    []LibraryGame
	steamByAppID map[string]LibraryGame
	lastRefresh  time.Time

	ignoredExes  map[string]struct{}
	ignoredGames map[string]struct{}
}

func NewDetector(cfg Config, verbose bool) (*Detector, error) {
	builtin, err := loadBuiltinGames()
	if err != nil {
		return nil, err
	}

	d := &Detector{
		cfg:          cfg,
		verbose:      verbose,
		builtin:      builtin,
		ignoredExes:  make(map[string]struct{}),
		ignoredGames: make(map[string]struct{}),
	}
	for _, exe := range defaultIgnoredExes {
		d.ignoredExes[strings.ToLower(exe)] = struct{}{}
	}
	for _, exe := range cfg.IgnoredExecutables {
		exe = strings.TrimSpace(exe)
		if exe != "" {
			d.ignoredExes[strings.ToLower(exe)] = struct{}{}
		}
	}
	for _, name := range cfg.IgnoredGames {
		name = strings.TrimSpace(name)
		if name != "" {
			d.ignoredGames[strings.ToLower(name)] = struct{}{}
		}
	}

	d.RefreshLibraries(true)
	return d, nil
}

func loadBuiltinGames() (map[string][]GamePreset, error) {
	var doc struct {
		Games []GamePreset `json:"games"`
	}
	if err := json.Unmarshal(builtinGamesJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse built-in game list: %w", err)
	}
	byExe := make(map[string][]GamePreset)
	for i := range doc.Games {
		preset := doc.Games[i]
		preset.Name = strings.TrimSpace(preset.Name)
		if preset.Name == "" {
			continue
		}
		for j := range preset.Executables {
			preset.Executables[j] = strings.TrimSpace(preset.Executables[j])
		}
		for _, exe := range preset.Executables {
			if exe == "" {
				continue
			}
			key := strings.ToLower(exe)
			byExe[key] = append(byExe[key], preset)
		}
	}
	return byExe, nil
}

func (d *Detector) RefreshLibraries(force bool) {
	interval := time.Duration(d.cfg.LibraryRefreshMinutes) * time.Minute
	if !force && !d.lastRefresh.IsZero() && time.Since(d.lastRefresh) < interval {
		return
	}
	games, warnings := scanLibraries(d.cfg, d.verbose)
	for _, warning := range warnings {
		log.Printf("library: %s", warning)
	}

	// A transient failure (disconnected drive, launcher update) should not wipe
	// a previously good library.
	if len(games) == 0 && len(d.libraries) > 0 {
		log.Printf("library: scan returned no games; keeping previous %d entries", len(d.libraries))
		d.lastRefresh = time.Now()
		return
	}

	steamByAppID := make(map[string]LibraryGame)
	for _, game := range games {
		if game.Source == "steam" && game.AppID != "" {
			if _, ok := steamByAppID[game.AppID]; !ok {
				steamByAppID[game.AppID] = game
			}
		}
	}
	d.libraries = games
	d.steamByAppID = steamByAppID
	d.lastRefresh = time.Now()

	counts := make(map[string]int)
	for _, game := range games {
		counts[game.Source]++
	}
	log.Printf("library: steam=%d epic=%d gog=%d ubisoft=%d",
		counts["steam"], counts["epic"], counts["gog"], counts["ubisoft"])
}

// Detect scans processes once and returns the best current game, or nil.
func (d *Detector) Detect(now time.Time) (*Game, error) {
	interval := time.Duration(d.cfg.LibraryRefreshMinutes) * time.Minute
	if d.lastRefresh.IsZero() || now.Sub(d.lastRefresh) >= interval {
		d.RefreshLibraries(true)
	}

	processes, system, err := scanProcesses()
	if err != nil {
		return nil, err
	}

	byPID := make(map[uint32]Process, len(processes))
	steamRunning := false
	matches := make([]*Game, 0, 4)
	myPID := uint32(os.Getpid())
	for _, proc := range processes {
		byPID[proc.PID] = proc
		if strings.EqualFold(proc.Exe, "steam.exe") {
			steamRunning = true
		}
		if proc.PID == myPID {
			continue
		}
		if !proc.Window.Visible {
			continue
		}
		if game := d.matchProcess(proc, system); game != nil {
			matches = append(matches, game)
		}
	}

	// Steam's RunningAppID is a useful fallback when process paths are not
	// readable (for example when the game runs elevated and we do not).
	if len(matches) == 0 && steamRunning {
		if appID := steamRunningAppID(); appID != "" {
			if library, ok := d.steamByAppID[appID]; ok {
				matches = append(matches, &Game{
					Name:       library.Name,
					Source:     "steam",
					AppID:      appID,
					Fullscreen: system.D3DFullscreen,
					Foreground: system.D3DFullscreen,
				})
			}
		}
	}

	// Optional heuristic for non-library games: anything exclusive/borderless
	// fullscreen and not obviously a browser or media player.
	if len(matches) == 0 && d.cfg.DetectUnknownFullscreen && system.ForegroundPID != 0 {
		if proc, ok := byPID[system.ForegroundPID]; ok && proc.Window.Visible &&
			(system.D3DFullscreen || proc.Window.Fullscreen) &&
			!d.ignoreAutoExe(proc.Exe, proc.Path) && !isNonGameFullscreen(proc.Exe) {
			name := strings.TrimSpace(proc.Window.Title)
			if name == "" {
				name = strings.TrimSuffix(proc.Exe, ".exe")
			}
			if !d.ignoredGame(name) {
				matches = append(matches, &Game{
					Name:       name,
					Source:     "unknown",
					Exe:        proc.Exe,
					PID:        proc.PID,
					Path:       proc.Path,
					Title:      proc.Window.Title,
					StartedAt:  timePtr(proc.StartedAt),
					Fullscreen: true,
					Foreground: true,
				})
			}
		}
	}

	matches = dedupeMatches(matches)
	if len(matches) == 0 {
		if d.verbose {
			log.Printf("detect: no game")
		}
		return nil, nil
	}

	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.Foreground != b.Foreground {
			return a.Foreground
		}
		if a.Fullscreen != b.Fullscreen {
			return a.Fullscreen
		}
		if (a.StartedAt == nil) != (b.StartedAt == nil) {
			return a.StartedAt != nil
		}
		if a.StartedAt != nil && !a.StartedAt.Equal(*b.StartedAt) {
			return a.StartedAt.After(*b.StartedAt)
		}
		return len(a.Path) > len(b.Path)
	})

	if d.verbose {
		log.Printf("detect: %s", formatCandidates(matches))
	}
	return matches[0], nil
}

func (d *Detector) matchProcess(proc Process, system SystemState) *Game {
	// 1. Explicit user configuration has priority.
	for i := range d.cfg.Games {
		custom := &d.cfg.Games[i]
		if customMatches(*custom, proc) && !d.ignoredGame(custom.Name) {
			return d.newGame(proc, custom.Name, "custom", custom.AppID, system)
		}
	}

	// 2. Installed launcher games matched by install directory. The list is
	// sorted deepest-first, so nested installs resolve correctly.
	for _, library := range d.libraries {
		if d.ignoredGame(library.Name) {
			continue
		}
		pathMatch := proc.Path != "" && pathInside(proc.Path, library.InstallDir) &&
			!d.ignoreAutoExe(proc.Exe, proc.Path)
		exeMatch := library.LaunchExe != "" && strings.EqualFold(proc.Exe, library.LaunchExe) &&
			(proc.Path == "" || pathInside(proc.Path, library.InstallDir))
		if pathMatch || exeMatch {
			return d.newGame(proc, library.Name, library.Source, library.AppID, system)
		}
	}

	// 3. Built-in executable names for standalone / non-Steam installs.
	if presets, ok := d.builtin[strings.ToLower(proc.Exe)]; ok && !d.ignoreAutoExe(proc.Exe, proc.Path) {
		for _, preset := range presets {
			if preset.MatchWindowTitle != "" &&
				!strings.Contains(strings.ToLower(proc.Window.Title), strings.ToLower(preset.MatchWindowTitle)) {
				continue
			}
			if !d.ignoredGame(preset.Name) {
				return d.newGame(proc, preset.Name, "known", preset.AppID, system)
			}
		}
	}

	return nil
}

func (d *Detector) newGame(proc Process, name, source, appID string, system SystemState) *Game {
	return &Game{
		Name:       name,
		Source:     source,
		AppID:      appID,
		Exe:        proc.Exe,
		PID:        proc.PID,
		Path:       proc.Path,
		Title:      proc.Window.Title,
		StartedAt:  timePtr(proc.StartedAt),
		Fullscreen: proc.Window.Fullscreen || (system.D3DFullscreen && proc.Window.Foreground),
		Foreground: proc.Window.Foreground,
	}
}

func (d *Detector) ignoreAutoExe(exe, path string) bool {
	lower := strings.ToLower(exe)
	if _, ok := d.ignoredExes[lower]; ok {
		return true
	}
	for _, fragment := range ignoredPathFragments {
		if strings.Contains(strings.ToLower(path), fragment) {
			return true
		}
	}
	for _, fragment := range ignoredExeFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	if strings.HasSuffix(lower, "launcher.exe") || strings.HasSuffix(lower, "launcher_x64.exe") ||
		strings.HasSuffix(lower, "launcher64.exe") || strings.HasSuffix(lower, "launcher32.exe") {
		return true
	}
	return false
}

func (d *Detector) ignoredGame(name string) bool {
	_, ok := d.ignoredGames[strings.ToLower(name)]
	return ok
}

func customMatches(c CustomGame, proc Process) bool {
	exeOK := len(c.Executables) == 0
	if !exeOK {
		for _, exe := range c.Executables {
			if strings.EqualFold(exe, proc.Exe) {
				exeOK = true
				break
			}
		}
	}
	if !exeOK {
		return false
	}
	if c.MatchWindowTitle != "" &&
		!strings.Contains(strings.ToLower(proc.Window.Title), strings.ToLower(c.MatchWindowTitle)) {
		return false
	}
	if c.InstallDir == "" {
		return true
	}
	if proc.Path != "" && pathInside(proc.Path, c.InstallDir) {
		return true
	}
	// If the executable path is inaccessible, an explicit exe name is still a
	// good enough fallback.
	return len(c.Executables) > 0
}

func dedupeMatches(matches []*Game) []*Game {
	byKey := make(map[string]*Game, len(matches))
	order := make([]string, 0, len(matches))
	for _, game := range matches {
		key := game.Key()
		old, ok := byKey[key]
		if !ok {
			byKey[key] = game
			order = append(order, key)
			continue
		}
		if game.Foreground && !old.Foreground {
			game.Fullscreen = game.Fullscreen || old.Fullscreen
			byKey[key] = game
			continue
		}
		old.Fullscreen = old.Fullscreen || game.Fullscreen
		old.Foreground = old.Foreground || game.Foreground
	}
	out := make([]*Game, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	c := t
	return &c
}

func formatCandidates(matches []*Game) string {
	parts := make([]string, 0, len(matches))
	for _, game := range matches {
		parts = append(parts, fmt.Sprintf("%s [%s] fg=%t fs=%t exe=%s",
			game.Name, game.Source, game.Foreground, game.Fullscreen, game.Exe))
	}
	return strings.Join(parts, "; ")
}

var defaultIgnoredExes = []string{
	"applicationframehost.exe",
	"battle.net.exe",
	"battleye.exe",
	"beservice.exe",
	"crashpad_handler.exe",
	"crashreportclient.exe",
	"discord.exe",
	"dwm.exe",
	"eaapp.exe",
	"eadesktop.exe",
	"epicgameslauncher.exe",
	"epicwebhelper.exe",
	"explorer.exe",
	"gameoverlayui.exe",
	"galaxyclient.exe",
	"goggalaxy.exe",
	"launcher.exe",
	"origin.exe",
	"riotclientservices.exe",
	"searchhost.exe",
	"shellexperiencehost.exe",
	"steam.exe",
	"steamservice.exe",
	"steamwebhelper.exe",
	"ubisoftconnect.exe",
	"unitycrashhandler32.exe",
	"unitycrashhandler64.exe",
	"uninstall.exe",
	"upc.exe",
	"vc_redist.x64.exe",
	"vc_redist.x86.exe",
}

var ignoredPathFragments = []string{
	`\battleye\`,
	`\crashreport\`,
	`\easyanticheat\`,
	`\installers\`,
	`\redist\`,
	`\steamworks\`,
	`\support\`,
	`\_commonredist\`,
}

var ignoredExeFragments = []string{
	"crashhandler",
	"crashpad",
	"crashreport",
	"dxsetup",
	"dxwebsetup",
	"easyanticheat",
	"eosbootstrapper",
	"epicwebhelper",
	"physx",
	"prereq",
	"redist",
	"steamwebhelper",
	"unins",
}

func isNonGameFullscreen(exe string) bool {
	_, ok := nonGameFullscreenExes[strings.ToLower(exe)]
	return ok
}

var nonGameFullscreenExes = map[string]struct{}{
	"acrobat.exe":              {},
	"applicationframehost.exe": {},
	"brave.exe":                {},
	"chrome.exe":               {},
	"chromium.exe":             {},
	"code.exe":                 {},
	"excel.exe":                {},
	"firefox.exe":              {},
	"floorp.exe":               {},
	"iexplore.exe":             {},
	"jellyfinmediaplayer.exe":  {},
	"librewolf.exe":            {},
	"mpc-be.exe":               {},
	"mpc-be64.exe":             {},
	"mpc-hc.exe":               {},
	"mpc-hc64.exe":             {},
	"mpv.exe":                  {},
	"ms-teams.exe":             {},
	"msedge.exe":               {},
	"obs32.exe":                {},
	"obs64.exe":                {},
	"onenote.exe":              {},
	"opera.exe":                {},
	"plexmediaplayer.exe":      {},
	"potplayer.exe":            {},
	"potplayermini64.exe":      {},
	"powerpnt.exe":             {},
	"smplayer.exe":             {},
	"spotify.exe":              {},
	"teams.exe":                {},
	"thorium.exe":              {},
	"vlc.exe":                  {},
	"video.ui.exe":             {},
	"vivaldi.exe":              {},
	"waterfox.exe":             {},
	"webexmta.exe":             {},
	"winword.exe":              {},
	"wmplayer.exe":             {},
	"zoom.exe":                 {},
}
