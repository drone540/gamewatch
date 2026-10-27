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
		{2 * time.Second, a, 1}, // start after 2 seconds
		{3 * time.Second, nil, 0},
		{7 * time.Second, nil, 0},
		{8 * time.Second, nil, 1}, // stop 5 seconds after last seen
	}
	for i, step := range steps {
		events := tracker.Update(start.Add(step.offset), step.game)
		if len(events) != step.events {
			t.Fatalf("step %d: got %d events, want %d: %+v", i, len(events), step.events, events)
		}
		if step.events == 1 && i == 5 {
			if events[0].Event != "game_stopped" {
				t.Fatalf("event = %q", events[0].Event)
			}
			if events[0].DurationSeconds != 6 {
				t.Fatalf("duration = %d, want 6", events[0].DurationSeconds)
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
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(events), events)
	}
	if events[0].Event != "game_stopped" || events[0].Game.Name != "A" {
		t.Fatalf("first event = %+v", events[0])
	}
	if events[1].Event != "game_started" || events[1].Game.Name != "B" {
		t.Fatalf("second event = %+v", events[1])
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
