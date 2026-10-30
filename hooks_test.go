package main

import (
	"testing"
	"time"
)

func TestHookEnv(t *testing.T) {
	start := time.Date(2026, 10, 27, 12, 37, 41, 0, time.UTC)
	game := &Game{
		Name:       "ELDEN RING",
		Source:     "steam",
		AppID:      "1245620",
		Exe:        "eldenring.exe",
		PID:        1234,
		Path:       `D:\Steam\ELDEN RING\eldenring.exe`,
		Title:      "ELDEN RING",
		Fullscreen: true,
		Foreground: true,
	}
	event := Event{
		Event:  "game_started",
		At:     start,
		Game:   game,
		Status: Status{Gaming: true, Game: game, Games: []*Game{game}},
	}

	env := envMap(hookEnv(event))
	checks := map[string]string{
		"GAMEWATCH_EVENT":            "game_started",
		"GAMEWATCH_AT":               "2026-10-27T12:37:41Z",
		"GAMEWATCH_GAMING":           "true",
		"GAMEWATCH_GAME_COUNT":       "1",
		"GAMEWATCH_GAME_NAME":        "ELDEN RING",
		"GAMEWATCH_GAME_SOURCE":      "steam",
		"GAMEWATCH_GAME_APPID":       "1245620",
		"GAMEWATCH_GAME_EXE":         "eldenring.exe",
		"GAMEWATCH_GAME_PID":         "1234",
		"GAMEWATCH_GAME_PATH":        `D:\Steam\ELDEN RING\eldenring.exe`,
		"GAMEWATCH_GAME_TITLE":       "ELDEN RING",
		"GAMEWATCH_GAME_FULLSCREEN":  "true",
		"GAMEWATCH_GAME_FOREGROUND":  "true",
		"GAMEWATCH_DURATION_SECONDS": "0",
	}
	for name, want := range checks {
		if got := env[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestHookEnvUsesPrimaryGameForModeEvent(t *testing.T) {
	game := &Game{Name: "Fortnite", Source: "epic", PID: 5678}
	event := Event{
		Event:  "gaming_started",
		Status: Status{Gaming: true, Game: game, Games: []*Game{game}},
	}

	env := envMap(hookEnv(event))
	if env["GAMEWATCH_GAME_NAME"] != "Fortnite" {
		t.Fatalf("mode event game name = %q", env["GAMEWATCH_GAME_NAME"])
	}
	if env["GAMEWATCH_GAME_COUNT"] != "1" {
		t.Fatalf("mode event game count = %q", env["GAMEWATCH_GAME_COUNT"])
	}
}

func TestAppendHookEnvReplacesInheritedValues(t *testing.T) {
	base := []string{
		"PATH=C:\\Windows",
		"GAMEWATCH_EVENT=stale",
		"GAMEWATCH_GAME_NAME=stale",
		"USER=tester",
	}
	env := appendHookEnv(base, []string{"GAMEWATCH_EVENT=game_started"})

	seen := 0
	for _, entry := range env {
		switch entry {
		case "GAMEWATCH_EVENT=game_started":
			seen++
		case "GAMEWATCH_EVENT=stale", "GAMEWATCH_GAME_NAME=stale":
			t.Fatalf("stale hook variable kept: %q", entry)
		}
	}
	if seen != 1 {
		t.Fatalf("GAMEWATCH_EVENT appears %d times", seen)
	}
	values := envMap(env)
	if values["PATH"] != `C:\Windows` || values["USER"] != "tester" {
		t.Fatalf("non-hook environment changed: %+v", values)
	}
}

func TestHookConfigNormalize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Hooks.GameStarted = "  python C:\\Scripts\\discord_game.py  "
	cfg.HookTimeoutSeconds = 0
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.Hooks.GameStarted != `python C:\Scripts\discord_game.py` {
		t.Fatalf("hook command = %q", cfg.Hooks.GameStarted)
	}
	if cfg.HookTimeoutSeconds != 30 {
		t.Fatalf("hook timeout = %d, want 30", cfg.HookTimeoutSeconds)
	}
	if !cfg.Hooks.configured() {
		t.Fatal("hooks should be configured")
	}
}

func envMap(env []string) map[string]string {
	values := make(map[string]string, len(env))
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				values[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	return values
}
