package main

import (
	"sort"
	"time"
)

// Tracker turns raw per-poll detections into debounced events. It tracks every
// running game, not just the foreground one, so closing one game does not end
// gaming mode while another is still active.
type Tracker struct {
	startDelay time.Duration
	stopDelay  time.Duration

	active     map[string]*trackedGame
	candidates map[string]time.Time
	modeSince  time.Time
}

type trackedGame struct {
	game     *Game
	since    time.Time
	lastSeen time.Time
	current  bool
	priority int
}

func NewTracker(startDelay, stopDelay time.Duration) *Tracker {
	if startDelay < 0 {
		startDelay = 0
	}
	if stopDelay < 0 {
		stopDelay = 0
	}
	return &Tracker{
		startDelay: startDelay,
		stopDelay:  stopDelay,
		active:     make(map[string]*trackedGame),
		candidates: make(map[string]time.Time),
	}
}

// Update is the single-game convenience wrapper used by callers that only care
// about the primary game.
func (t *Tracker) Update(now time.Time, detected *Game) []Event {
	if detected == nil {
		return t.UpdateAll(now, nil)
	}
	return t.UpdateAll(now, []*Game{detected})
}

// UpdateAll applies one poll containing every currently detected game and
// returns any events caused by the transition.
func (t *Tracker) UpdateAll(now time.Time, detected []*Game) []Event {
	t.init()

	wasGaming := len(t.active) > 0
	current := make([]*Game, 0, len(detected))
	seen := make(map[string]struct{}, len(detected))
	for _, game := range detected {
		if game == nil {
			continue
		}
		key := game.Key()
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		current = append(current, game)
	}

	for key := range t.candidates {
		if _, ok := seen[key]; !ok {
			delete(t.candidates, key)
		}
	}
	for _, tracked := range t.active {
		tracked.current = false
	}

	var events []Event
	for i, game := range current {
		key := game.Key()
		if tracked, ok := t.active[key]; ok {
			tracked.game = game.clone()
			tracked.lastSeen = now
			tracked.current = true
			tracked.priority = i
			delete(t.candidates, key)
			continue
		}

		candidateSince, ok := t.candidates[key]
		if !ok {
			candidateSince = now
			t.candidates[key] = now
		}
		if now.Sub(candidateSince) < t.startDelay {
			continue
		}

		tracked := &trackedGame{
			game:     game.clone(),
			since:    now,
			lastSeen: now,
			current:  true,
			priority: i,
		}
		t.active[key] = tracked
		delete(t.candidates, key)
		if t.modeSince.IsZero() {
			t.modeSince = now
		}

		events = append(events, t.startEvent(now, tracked))
	}

	// Emit the mode transition after all game starts so its status includes
	// every game that became active in this poll.
	if !wasGaming && len(t.active) > 0 {
		events = append(events, t.gamingStartEvent(now))
	}

	var stopped []*trackedGame
	for key, tracked := range t.active {
		if now.Sub(tracked.lastSeen) > t.stopDelay {
			stopped = append(stopped, tracked)
			delete(t.active, key)
		}
	}
	sort.Slice(stopped, func(i, j int) bool {
		if stopped[i].since.Equal(stopped[j].since) {
			return stopped[i].game.Key() < stopped[j].game.Key()
		}
		return stopped[i].since.Before(stopped[j].since)
	})
	for _, tracked := range stopped {
		events = append(events, t.stopEvent(now, tracked))
	}
	if len(stopped) > 0 && len(t.active) == 0 {
		events = append(events, t.gamingStopEvent(now))
	}

	return events
}

func (t *Tracker) init() {
	if t.active == nil {
		t.active = make(map[string]*trackedGame)
	}
	if t.candidates == nil {
		t.candidates = make(map[string]time.Time)
	}
}

func (t *Tracker) startEvent(now time.Time, tracked *trackedGame) Event {
	return Event{
		Event:  "game_started",
		At:     now,
		Game:   tracked.game.clone(),
		Status: t.Status(now),
	}
}

func (t *Tracker) stopEvent(now time.Time, tracked *trackedGame) Event {
	duration := int64(0)
	if !tracked.since.IsZero() {
		duration = int64(now.Sub(tracked.since).Seconds())
	}
	return Event{
		Event:           "game_stopped",
		At:              now,
		Game:            tracked.game.clone(),
		DurationSeconds: duration,
		Status:          t.Status(now),
	}
}

func (t *Tracker) gamingStartEvent(now time.Time) Event {
	return Event{
		Event:  "gaming_started",
		At:     now,
		Status: t.Status(now),
	}
}

func (t *Tracker) gamingStopEvent(now time.Time) Event {
	duration := int64(0)
	if !t.modeSince.IsZero() {
		duration = int64(now.Sub(t.modeSince).Seconds())
	}
	t.modeSince = time.Time{}
	return Event{
		Event:           "gaming_stopped",
		At:              now,
		DurationSeconds: duration,
		Status:          t.Status(now),
	}
}

func (t *Tracker) Status(now time.Time) Status {
	t.init()

	tracked := make([]*trackedGame, 0, len(t.active))
	for _, game := range t.active {
		tracked = append(tracked, game)
	}
	sort.SliceStable(tracked, func(i, j int) bool {
		a, b := tracked[i], tracked[j]
		if a.current != b.current {
			return a.current
		}
		if a.current && a.priority != b.priority {
			return a.priority < b.priority
		}
		if !a.since.Equal(b.since) {
			return a.since.Before(b.since)
		}
		return a.game.Key() < b.game.Key()
	})

	games := make([]*Game, 0, len(tracked))
	for _, tracked := range tracked {
		games = append(games, tracked.game.clone())
	}
	if len(games) == 0 {
		return Status{CheckedAt: now}
	}

	status := Status{
		Gaming:    true,
		Games:     games,
		CheckedAt: now,
	}
	status.Game = games[0]
	since := t.modeSince
	if since.IsZero() {
		since = now
	}
	status.Since = &since
	status.DurationSeconds = int64(now.Sub(since).Seconds())
	if status.DurationSeconds < 0 {
		status.DurationSeconds = 0
	}
	return status
}
