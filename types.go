package main

import (
	"strings"
	"time"
)

// Game is one detected, currently running game.
type Game struct {
	Name       string     `json:"name"`
	Source     string     `json:"source,omitempty"` // steam, epic, gog, ubisoft, known, custom, unknown
	AppID      string     `json:"appId,omitempty"`
	Exe        string     `json:"exe,omitempty"`
	PID        uint32     `json:"pid,omitempty"`
	Path       string     `json:"path,omitempty"`
	Title      string     `json:"title,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	Fullscreen bool       `json:"fullscreen"`
	Foreground bool       `json:"foreground"`
}

// Key identifies a game independently of its PID, so restarting a game or
// switching between its launcher and main executable does not look like a new
// game.
func (g *Game) Key() string {
	if g.Source != "" && g.AppID != "" {
		return g.Source + "|" + g.AppID
	}
	if g.Source == "unknown" {
		// Window titles change with levels/menus, so use the executable path
		// to keep one stable session for an unknown fullscreen app.
		return "unknown|" + strings.ToLower(g.Exe) + "|" + strings.ToLower(g.Path)
	}
	// Names are stable across DX11/DX12 and launcher/main executable switches.
	return g.Source + "|" + strings.ToLower(g.Name)
}

func (g *Game) clone() *Game {
	if g == nil {
		return nil
	}
	c := *g
	return &c
}

// Status is the current detector state. It is safe to expose as JSON.
type Status struct {
	Gaming          bool       `json:"gaming"`
	Game            *Game      `json:"game,omitempty"`
	Since           *time.Time `json:"since,omitempty"`
	DurationSeconds int64      `json:"durationSeconds"`
	CheckedAt       time.Time  `json:"checkedAt"`
}

// Event is emitted whenever the tracked game changes.
type Event struct {
	Event           string    `json:"event"` // game_started, game_stopped
	At              time.Time `json:"at"`
	Game            *Game     `json:"game,omitempty"`
	DurationSeconds int64     `json:"durationSeconds,omitempty"`
	Status          Status    `json:"status"`
}

// Process is a lightweight snapshot of a running process.
type Process struct {
	PID       uint32
	Exe       string
	Path      string
	StartedAt time.Time
	Window    WindowInfo
}

// WindowInfo describes the best top-level window owned by a process.
type WindowInfo struct {
	Title      string
	Visible    bool
	Foreground bool
	Fullscreen bool
	Width      int
	Height     int
}

// SystemState contains global desktop state relevant to game detection.
type SystemState struct {
	ForegroundPID uint32
	D3DFullscreen bool
}

// LibraryGame is a game installed through a launcher. Path matching against
// InstallDir is the main way games are identified, so no executable scanning
// of the game folder is needed.
type LibraryGame struct {
	Name       string
	Source     string
	AppID      string
	InstallDir string
	LaunchExe  string
}
