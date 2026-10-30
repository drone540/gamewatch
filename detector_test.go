package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPathInside(t *testing.T) {
	tests := []struct {
		child  string
		parent string
		want   bool
	}{
		{`C:\Games\Foo\foo.exe`, `C:\Games\Foo`, true},
		{`c:\games\foo\bin\foo.exe`, `C:\Games\Foo`, true},
		{`C:/Games/Foo/bin/foo.exe`, `C:\Games\Foo\`, true},
		{`C:\Games\FooBar\foo.exe`, `C:\Games\Foo`, false},
		{`C:\Games\Foo`, `C:\Games\Foo`, true},
		{`C:\Games\Foo`, ``, false},
		{``, `C:\Games\Foo`, false},
	}
	for _, test := range tests {
		if got := pathInside(test.child, test.parent); got != test.want {
			t.Errorf("pathInside(%q, %q) = %t, want %t", test.child, test.parent, got, test.want)
		}
	}
}

func game(name string) *Game {
	return &Game{Name: name, Source: "steam", AppID: name, Exe: name + ".exe"}
}

func TestTrackerStartAndStopDelay(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(2*time.Second, 5*time.Second)
	a := game("A")

	steps := []struct {
		offset time.Duration
		game   *Game
		events int
	}{
		{0, a, 0},
		{1 * time.Second, a, 0},
		{2 * time.Second, a, 2}, // game_started, gaming_started
		{3 * time.Second, nil, 0},
		{7 * time.Second, nil, 0},
		{8 * time.Second, nil, 2}, // game_stopped, gaming_stopped
	}
	for i, step := range steps {
		events := tracker.Update(start.Add(step.offset), step.game)
		if len(events) != step.events {
			t.Fatalf("step %d: got %d events, want %d: %+v", i, len(events), step.events, events)
		}
		switch i {
		case 2:
			if events[0].Event != "game_started" || events[1].Event != "gaming_started" {
				t.Fatalf("start event order = %+v", events)
			}
		case 5:
			if events[0].Event != "game_stopped" || events[0].DurationSeconds != 6 {
				t.Fatalf("stop event = %+v", events[0])
			}
			if events[1].Event != "gaming_stopped" || events[1].DurationSeconds != 6 {
				t.Fatalf("mode stop event = %+v", events[1])
			}
		}
	}
}

func TestTrackerSwitch(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(2*time.Second, 10*time.Second)

	tracker.Update(start, game("A"))
	tracker.Update(start.Add(2*time.Second), game("A"))

	events := tracker.Update(start.Add(3*time.Second), game("B"))
	if len(events) != 0 {
		t.Fatalf("B should still be pending: %+v", events)
	}
	events = tracker.Update(start.Add(5*time.Second), game("B"))
	if len(events) != 1 || events[0].Event != "game_started" || events[0].Game.Name != "B" {
		t.Fatalf("B start event = %+v", events)
	}
	if !events[0].Status.Gaming || len(events[0].Status.Games) != 2 {
		t.Fatalf("A should still be active while B starts: %+v", events[0].Status)
	}

	// A has not been seen since 12:00:02, so it stops after the 10s delay.
	events = tracker.Update(start.Add(13*time.Second), game("B"))
	if len(events) != 1 || events[0].Event != "game_stopped" || events[0].Game.Name != "A" {
		t.Fatalf("A stop event = %+v", events)
	}
	if !events[0].Status.Gaming || events[0].Status.Game.Name != "B" {
		t.Fatalf("gaming should continue with B: %+v", events[0].Status)
	}
}

func TestTrackerKeepsGamingWithMultipleGames(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(0, 5*time.Second)
	a, b := game("A"), game("B")

	events := tracker.UpdateAll(start, []*Game{a, b})
	if len(events) != 3 {
		t.Fatalf("initial events = %+v", events)
	}
	if events[0].Event != "game_started" || events[0].Game.Name != "A" ||
		events[1].Event != "game_started" || events[1].Game.Name != "B" ||
		events[2].Event != "gaming_started" {
		t.Fatalf("initial event order = %+v", events)
	}
	if len(events[2].Status.Games) != 2 {
		t.Fatalf("gaming_started should include both games: %+v", events[2].Status)
	}

	status := tracker.Status(start.Add(time.Second))
	if !status.Gaming || len(status.Games) != 2 || status.Game.Name != "A" {
		t.Fatalf("both games should be active: %+v", status)
	}

	// Close A but keep B running. The primary game switches to B, but gaming
	// mode must stay active.
	events = tracker.UpdateAll(start.Add(time.Second), []*Game{b})
	if len(events) != 0 {
		t.Fatalf("events while A is within stop delay: %+v", events)
	}
	status = tracker.Status(start.Add(time.Second))
	if !status.Gaming || len(status.Games) != 2 || status.Game.Name != "B" {
		t.Fatalf("B should be primary while A is still tracked: %+v", status)
	}

	events = tracker.UpdateAll(start.Add(7*time.Second), []*Game{b})
	if len(events) != 1 || events[0].Event != "game_stopped" || events[0].Game.Name != "A" {
		t.Fatalf("A stop event = %+v", events)
	}
	if !events[0].Status.Gaming {
		t.Fatalf("gaming stopped while B is still running: %+v", events[0].Status)
	}

	events = tracker.UpdateAll(start.Add(13*time.Second), nil)
	if len(events) != 2 {
		t.Fatalf("final events = %+v", events)
	}
	if events[0].Event != "game_stopped" || events[0].Game.Name != "B" ||
		events[1].Event != "gaming_stopped" {
		t.Fatalf("final event order = %+v", events)
	}
	status = tracker.Status(start.Add(13 * time.Second))
	if status.Gaming || status.Game != nil || status.Games != nil || status.Since != nil {
		t.Fatalf("status after last game stopped = %+v", status)
	}
}

func TestTrackerKeepsGameAcrossRestart(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewTracker(0, time.Minute)
	tracker.Update(start, game("A"))

	restarted := game("A")
	restarted.PID = 999
	events := tracker.Update(start.Add(time.Second), restarted)
	if len(events) != 0 {
		t.Fatalf("same game should not restart: %+v", events)
	}
	status := tracker.Status(start.Add(time.Second))
	if status.Game.PID != 999 {
		t.Fatalf("PID = %d, want 999", status.Game.PID)
	}
}

func TestUnknownGameKeyIgnoresTitle(t *testing.T) {
	first := &Game{Name: "Loading...", Source: "unknown", Exe: "foo.exe", Path: `C:\Foo\foo.exe`}
	second := *first
	second.Name = "Main Menu"
	if first.Key() != second.Key() {
		t.Fatalf("unknown game key changed with title: %q != %q", first.Key(), second.Key())
	}
}

func TestGameKeyStableAcrossExecutables(t *testing.T) {
	first := &Game{Name: "Elden Ring", Source: "known", Exe: "eldenring.exe"}
	second := &Game{Name: "Elden Ring", Source: "known", Exe: "start_protected_game.exe"}
	if first.Key() != second.Key() {
		t.Fatalf("key changed with executable: %q != %q", first.Key(), second.Key())
	}
}

func TestDefaultConfigMerge(t *testing.T) {
	cfg := DefaultConfig()
	if err := json.Unmarshal([]byte(`{"pollSeconds":3,"steam":false}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.PollSeconds != 3 {
		t.Fatalf("PollSeconds = %d", cfg.PollSeconds)
	}
	if cfg.Steam {
		t.Fatal("Steam should be false")
	}
	if !cfg.Epic || cfg.StartDelaySeconds != 10 {
		t.Fatalf("defaults not preserved: %+v", cfg)
	}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinGamesLoad(t *testing.T) {
	byExe, err := loadBuiltinGames()
	if err != nil {
		t.Fatal(err)
	}
	presets := byExe["eldenring.exe"]
	if len(presets) == 0 || presets[0].Name != "Elden Ring" {
		t.Fatalf("missing Elden Ring: %+v", presets)
	}
	minecraft := byExe["javaw.exe"]
	if len(minecraft) == 0 || minecraft[0].MatchWindowTitle != "Minecraft" {
		t.Fatalf("Minecraft title filter missing: %+v", minecraft)
	}
}

func TestCustomMatches(t *testing.T) {
	proc := Process{
		Exe:    "MyGame.exe",
		Path:   `D:\Games\MyGame\MyGame.exe`,
		Window: WindowInfo{Title: "My Game - Level 1"},
	}
	if !customMatches(CustomGame{Name: "x", InstallDir: `D:\Games\MyGame`}, proc) {
		t.Fatal("expected install dir match")
	}
	if !customMatches(CustomGame{Name: "x", Executables: []string{"mygame.exe"}}, proc) {
		t.Fatal("expected case-insensitive exe match")
	}
	if customMatches(CustomGame{Name: "x", Executables: []string{"other.exe"}}, proc) {
		t.Fatal("unexpected exe match")
	}
	if customMatches(CustomGame{Name: "x", Executables: []string{"MyGame.exe"}, MatchWindowTitle: "Wrong title"}, proc) {
		t.Fatal("unexpected title match")
	}
}

func TestFormatDuration(t *testing.T) {
	if got := formatDuration(42); got != "42s" {
		t.Fatalf("42s = %q", got)
	}
	if got := formatDuration(125); got != "2m5s" {
		t.Fatalf("125s = %q", got)
	}
}

func testDetector(t *testing.T) *Detector {
	t.Helper()
	builtin, err := loadBuiltinGames()
	if err != nil {
		t.Fatal(err)
	}
	return &Detector{
		cfg: Config{Games: []CustomGame{
			{Name: "Custom Game", Executables: []string{"customgame.exe"}},
			{Name: "Ignored Custom", Executables: []string{"ignoredcustom.exe"}},
		}},
		builtin:     builtin,
		libraries:   dedupeLibraries([]LibraryGame{{Name: "Steam Game", Source: "steam", AppID: "1", InstallDir: `D:\Steam\Steam Game`}}),
		ignoredExes: map[string]struct{}{"launcher.exe": {}},
		// Keys are stored lowercased by NewDetector; keep test fixtures in the
		// same normalized form.
		ignoredGames: map[string]struct{}{
			"ignored custom": {},
			"steam game":     {},
		},
	}
}

func TestMatchProcess(t *testing.T) {
	d := testDetector(t)
	system := SystemState{}
	steamProc := Process{
		PID:    10,
		Exe:    "game.exe",
		Path:   `D:\Steam\Steam Game\game.exe`,
		Window: WindowInfo{Title: "Game", Visible: true, Foreground: true},
	}
	if game := d.matchProcess(steamProc, system); game != nil {
		t.Fatalf("ignored game matched: %+v", game)
	}

	d.ignoredGames = map[string]struct{}{"ignored custom": {}}
	game := d.matchProcess(steamProc, system)
	if game == nil || game.Name != "Steam Game" || game.Source != "steam" {
		t.Fatalf("steam path match = %+v", game)
	}

	launcher := steamProc
	launcher.Exe = "Launcher.exe"
	if game := d.matchProcess(launcher, system); game != nil {
		t.Fatalf("launcher matched: %+v", game)
	}

	custom := steamProc
	custom.Exe = "customgame.exe"
	custom.Path = `D:\Steam\Steam Game\customgame.exe`
	if game := d.matchProcess(custom, system); game == nil || game.Name != "Custom Game" || game.Source != "custom" {
		t.Fatalf("custom precedence = %+v", game)
	}

	known := steamProc
	known.Exe = "eldenring.exe"
	known.Path = `C:\Elsewhere\eldenring.exe`
	if game := d.matchProcess(known, system); game == nil || game.Name != "Elden Ring" || game.Source != "known" {
		t.Fatalf("builtin match = %+v", game)
	}
}

func TestDedupeMatches(t *testing.T) {
	first := &Game{Name: "A", Source: "steam", AppID: "1", Exe: "a.exe", Fullscreen: true}
	second := &Game{Name: "A", Source: "steam", AppID: "1", Exe: "a.exe", Foreground: true}
	out := dedupeMatches([]*Game{first, second})
	if len(out) != 1 {
		t.Fatalf("got %d matches", len(out))
	}
	if !out[0].Foreground || !out[0].Fullscreen {
		t.Fatalf("flags not merged: %+v", out[0])
	}
}
