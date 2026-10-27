package main

import "time"

// Tracker turns raw per-poll detections into debounced game_started /
// game_stopped events. Start delay avoids launcher flashes; stop delay avoids
// alt-tab or loading-screen churn.
type Tracker struct {
	startDelay time.Duration
	stopDelay  time.Duration

	active      *Game
	activeSince time.Time
	lastSeen    time.Time

	candidate      *Game
	candidateSince time.Time
}

func NewTracker(startDelay, stopDelay time.Duration) *Tracker {
	if startDelay < 0 {
		startDelay = 0
	}
	if stopDelay < 0 {
		stopDelay = 0
	}
	return &Tracker{startDelay: startDelay, stopDelay: stopDelay}
}

func (t *Tracker) Update(now time.Time, detected *Game) []Event {
	var events []Event

	switch {
	case detected == nil:
		t.candidate = nil

	case t.active != nil && detected.Key() == t.active.Key():
		t.active = detected.clone()
		t.lastSeen = now
		t.candidate = nil

	default:
		if t.candidate == nil || t.candidate.Key() != detected.Key() {
			t.candidate = detected.clone()
			t.candidateSince = now
		}
		if now.Sub(t.candidateSince) >= t.startDelay {
			if t.active != nil {
				events = append(events, t.stopEvent(now))
			}
			t.active = detected.clone()
			t.activeSince = now
			t.lastSeen = now
			t.candidate = nil
			events = append(events, t.startEvent(now))
		}
	}

	// The active game may have disappeared (or another game is now in front).
	// Keep it for stopDelay so brief focus changes do not end the session.
	if t.active != nil && now.After(t.lastSeen) && now.Sub(t.lastSeen) > t.stopDelay {
		events = append(events, t.stopEvent(now))
	}

	return events
}

func (t *Tracker) startEvent(now time.Time) Event {
	status := t.Status(now)
	return Event{
		Event:  "game_started",
		At:     now,
		Game:   t.active.clone(),
		Status: status,
	}
}

func (t *Tracker) stopEvent(now time.Time) Event {
	game := t.active.clone()
	duration := int64(0)
	if !t.activeSince.IsZero() {
		duration = int64(now.Sub(t.activeSince).Seconds())
	}
	t.active = nil
	t.activeSince = time.Time{}
	t.lastSeen = time.Time{}
	status := t.Status(now)
	return Event{
		Event:           "game_stopped",
		At:              now,
		Game:            game,
		DurationSeconds: duration,
		Status:          status,
	}
}

func (t *Tracker) Status(now time.Time) Status {
	status := Status{Gaming: t.active != nil, CheckedAt: now}
	if t.active == nil {
		return status
	}
	status.Game = t.active.clone()
	since := t.activeSince
	status.Since = &since
	status.DurationSeconds = int64(now.Sub(t.activeSince).Seconds())
	return status
}
