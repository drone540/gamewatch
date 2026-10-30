package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func (a *Agent) runHook(event Event) {
	command := a.hookFor(event.Event)
	if command == "" {
		return
	}

	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("hook %s: encode event: %v", event.Event, err)
		return
	}

	timeout := time.Duration(a.cfg.HookTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := shellCommand(ctx, command)
	cmd.Env = appendHookEnv(os.Environ(), hookEnv(event))
	cmd.Stdin = bytes.NewReader(append(payload, '\n'))
	// Keep hook output off stdout so -json output stays valid JSONL.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			log.Printf("hook %s: timed out after %s", event.Event, timeout)
			return
		}
		log.Printf("hook %s: %v", event.Event, err)
	}
}

func (a *Agent) hookFor(event string) string {
	switch event {
	case "gaming_started":
		return a.cfg.Hooks.GamingStarted
	case "game_started":
		return a.cfg.Hooks.GameStarted
	case "game_stopped":
		return a.cfg.Hooks.GameStopped
	case "gaming_stopped":
		return a.cfg.Hooks.GamingStopped
	default:
		return ""
	}
}

// shellCommand runs configured commands through the platform shell, so paths,
// pipes, redirection, and quoted arguments behave as users expect.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.CommandContext(ctx, "cmd.exe", "/C", command)
	}
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}

func hookEnv(event Event) []string {
	game := event.Game
	if game == nil {
		game = event.Status.Game
	}

	env := []string{
		"GAMEWATCH_EVENT=" + event.Event,
		"GAMEWATCH_AT=" + event.At.Format(time.RFC3339Nano),
		"GAMEWATCH_GAMING=" + strconv.FormatBool(event.Status.Gaming),
		"GAMEWATCH_GAME_COUNT=" + strconv.Itoa(len(event.Status.Games)),
		"GAMEWATCH_DURATION_SECONDS=" + strconv.FormatInt(event.DurationSeconds, 10),
	}
	if game == nil {
		return append(env,
			"GAMEWATCH_GAME_NAME=",
			"GAMEWATCH_GAME_SOURCE=",
			"GAMEWATCH_GAME_APPID=",
			"GAMEWATCH_GAME_EXE=",
			"GAMEWATCH_GAME_PID=",
			"GAMEWATCH_GAME_PATH=",
			"GAMEWATCH_GAME_TITLE=",
			"GAMEWATCH_GAME_FULLSCREEN=",
			"GAMEWATCH_GAME_FOREGROUND=",
		)
	}

	pid := ""
	if game.PID != 0 {
		pid = strconv.FormatUint(uint64(game.PID), 10)
	}
	return append(env,
		"GAMEWATCH_GAME_NAME="+game.Name,
		"GAMEWATCH_GAME_SOURCE="+game.Source,
		"GAMEWATCH_GAME_APPID="+game.AppID,
		"GAMEWATCH_GAME_EXE="+game.Exe,
		"GAMEWATCH_GAME_PID="+pid,
		"GAMEWATCH_GAME_PATH="+game.Path,
		"GAMEWATCH_GAME_TITLE="+game.Title,
		"GAMEWATCH_GAME_FULLSCREEN="+strconv.FormatBool(game.Fullscreen),
		"GAMEWATCH_GAME_FOREGROUND="+strconv.FormatBool(game.Foreground),
	)
}

// appendHookEnv replaces inherited GAMEWATCH_* variables so a hook always sees
// the current event rather than stale values from the parent process.
func appendHookEnv(base, extra []string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, entry := range base {
		name := entry
		if i := strings.IndexByte(entry, '='); i >= 0 {
			name = entry[:i]
		}
		if strings.HasPrefix(strings.ToUpper(name), "GAMEWATCH_") {
			continue
		}
		out = append(out, entry)
	}
	return append(out, extra...)
}
