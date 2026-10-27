package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Agent struct {
	cfg      Config
	tracker  *Tracker
	detector *Detector

	mu     sync.Mutex
	status Status

	jsonOut      bool
	webhook      string
	statusFile   string
	client       *http.Client
	lastWriteErr string
}

func NewAgent(cfg Config, detector *Detector, jsonOut bool, webhook, statusFile string) *Agent {
	now := time.Now()
	return &Agent{
		cfg:        cfg,
		tracker:    NewTracker(time.Duration(cfg.StartDelaySeconds)*time.Second, time.Duration(cfg.StopDelaySeconds)*time.Second),
		detector:   detector,
		status:     Status{CheckedAt: now},
		jsonOut:    jsonOut,
		webhook:    webhook,
		statusFile: statusFile,
		client:     &http.Client{Timeout: 5 * time.Second},
	}
}

func (a *Agent) Run(ctx context.Context) {
	log.Printf("watching: poll=%ds startDelay=%ds stopDelay=%ds",
		a.cfg.PollSeconds, a.cfg.StartDelaySeconds, a.cfg.StopDelaySeconds)

	a.Tick(time.Now())
	ticker := time.NewTicker(time.Duration(a.cfg.PollSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("stopped")
			return
		case now := <-ticker.C:
			a.Tick(now)
		}
	}
}

func (a *Agent) Tick(now time.Time) {
	detected, err := a.detector.Detect(now)
	if err != nil {
		log.Printf("detect: %v", err)
		a.mu.Lock()
		a.status.CheckedAt = now
		a.mu.Unlock()
		return
	}
	events := a.tracker.Update(now, detected)

	status := a.tracker.Status(now)
	a.mu.Lock()
	a.status = status
	a.mu.Unlock()

	for _, event := range events {
		a.emit(event)
	}
	a.writeStatus(status)
}

func (a *Agent) Once(now time.Time) (Status, error) {
	detected, err := a.detector.Detect(now)
	if err != nil {
		return Status{}, err
	}
	tracker := NewTracker(0, 0)
	tracker.Update(now, detected)
	return tracker.Status(now), nil
}

func (a *Agent) Status() Status {
	a.mu.Lock()
	status := a.status
	a.mu.Unlock()

	if status.Gaming && status.Since != nil {
		now := time.Now()
		status.DurationSeconds = int64(now.Sub(*status.Since).Seconds())
		if status.DurationSeconds < 0 {
			status.DurationSeconds = 0
		}
		status.CheckedAt = now
	}
	return status
}

func (a *Agent) emit(event Event) {
	if a.jsonOut {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		_ = encoder.Encode(event)
	} else {
		fmt.Println(formatEvent(event))
	}
	if a.webhook != "" {
		go a.postEvent(event)
	}
}

func (a *Agent) postEvent(event Event) {
	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("webhook: encode: %v", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, a.webhook, bytes.NewReader(body))
	if err != nil {
		log.Printf("webhook: build request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "gamewatch")

	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("webhook: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("webhook: %s", resp.Status)
	}
}

func (a *Agent) writeStatus(status Status) {
	if a.statusFile == "" {
		return
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		a.logWriteError(err)
		return
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(a.statusFile), 0o755); err != nil {
		a.logWriteError(err)
		return
	}
	tmp := a.statusFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		a.logWriteError(err)
		return
	}
	if err := os.Rename(tmp, a.statusFile); err != nil {
		a.logWriteError(err)
		return
	}
	a.lastWriteErr = ""
}

func (a *Agent) logWriteError(err error) {
	message := err.Error()
	if message == a.lastWriteErr {
		return
	}
	a.lastWriteErr = message
	log.Printf("status file: %v", err)
}

func formatEvent(event Event) string {
	stamp := event.At.Local().Format("15:04:05")
	switch event.Event {
	case "game_started":
		if event.Game == nil {
			return fmt.Sprintf("[%s] game started", stamp)
		}
		return fmt.Sprintf("[%s] game started: %s%s", stamp, event.Game.Name, gameSuffix(event.Game))
	case "game_stopped":
		name := "game"
		if event.Game != nil {
			name = event.Game.Name
		}
		return fmt.Sprintf("[%s] game stopped: %s after %s",
			stamp, name, formatDuration(event.DurationSeconds))
	default:
		return fmt.Sprintf("[%s] %s", stamp, event.Event)
	}
}

func formatStatus(status Status) string {
	if !status.Gaming || status.Game == nil {
		return "not gaming"
	}
	return fmt.Sprintf("gaming: %s%s", status.Game.Name, gameSuffix(status.Game))
}

func gameSuffix(game *Game) string {
	if game == nil {
		return ""
	}
	var parts []string
	if game.Source != "" {
		source := game.Source
		if game.AppID != "" {
			source += " " + game.AppID
		}
		parts = append(parts, source)
	}
	if game.Exe != "" {
		parts = append(parts, game.Exe)
	}
	if game.Foreground {
		parts = append(parts, "foreground")
	}
	if game.Fullscreen {
		parts = append(parts, "fullscreen")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + joinComma(parts) + ")"
}

func joinComma(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ", "
		}
		out += value
	}
	return out
}

func formatDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	d := time.Duration(seconds) * time.Second
	if d < time.Minute {
		return fmt.Sprintf("%ds", seconds)
	}
	return d.Truncate(time.Second).String()
}

func startHTTPServer(ctx context.Context, addr string, agent *Agent) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status := agent.Status()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.URL.Query().Get("pretty") != "" {
			encoder := json.NewEncoder(w)
			encoder.SetIndent("", "  ")
			_ = encoder.Encode(status)
			return
		}
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/status", http.StatusTemporaryRedirect)
	})

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("http: %v", err)
		}
	}()
	log.Printf("status: http://%s/status", listener.Addr())
	return nil
}
