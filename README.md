# gamewatch

A small Windows CLI written in Go that detects when a game is being played.

It combines four passive signals:

1. **Launcher libraries** - reads Steam `appmanifest_*.acf` files (including extra
   library folders), Epic manifests, GOG and Ubisoft registry entries.
2. **Process paths** - a running executable is matched against the install
   directory of an installed game. This avoids walking every game folder.
3. **Executable names** - a built-in list covers common standalone / non-Steam
   games, and `gamewatch.json` can add your own.
4. **Windows state** - foreground window, borderless/exclusive fullscreen, and
   Steam's `RunningAppID` fallback.

State changes are debounced, so a launcher flash or a quick alt-tab does not
produce start/stop spam. All running games are tracked at once: closing one game
does not end gaming mode while another is still active.

Events are emitted when gaming mode starts/stops and when each individual game
starts/stops. Each event can run a shell command or be POSTed to a webhook.

## Build

Requires Go 1.22 or newer on any OS (the binary itself targets Windows).

```bat
cd gamewatch
go build -o gamewatch.exe .
```

Optional smaller binary:

```bat
go build -ldflags "-s -w" -o gamewatch.exe .
```

## Run

```bat
gamewatch.exe
```

Default output is human-readable on stdout; logs go to stderr:

```text
12:34:56 config: built-in defaults
12:34:56 library: steam=84 epic=6 gog=3 ubisoft=1
12:34:56 watching: poll=5s startDelay=10s stopDelay=20s
12:37:41 game started: ELDEN RING (steam 1245620, eldenring.exe, foreground, fullscreen)
12:37:41 gaming started: ELDEN RING (steam 1245620, eldenring.exe, foreground, fullscreen)
13:10:02 game started: Fortnite (epic, FortniteClient-Win64-Shipping.exe, fullscreen)
13:25:00 game stopped: ELDEN RING after 47m19s (still gaming: Fortnite)
13:40:00 game stopped: Fortnite after 29m58s
13:40:00 gaming stopped after 1h2m19s
```

Use `-json` for one JSON event per line:

```bat
gamewatch.exe -json
```

```json
{"event":"game_started","at":"2026-10-27T12:37:41Z","game":{"name":"ELDEN RING","source":"steam","appId":"1245620","exe":"eldenring.exe","pid":1234,"fullscreen":true,"foreground":true},"status":{"gaming":true,"game":{"name":"ELDEN RING","source":"steam","appId":"1245620","exe":"eldenring.exe","pid":1234,"fullscreen":true,"foreground":true},"games":[{"name":"ELDEN RING","source":"steam","appId":"1245620","exe":"eldenring.exe","pid":1234,"fullscreen":true,"foreground":true}],"since":"2026-10-27T12:37:41Z","durationSeconds":0,"checkedAt":"2026-10-27T12:37:41Z"}}
```

Check once and use the exit code in a script:

```bat
gamewatch.exe -once
if %ERRORLEVEL% EQU 0 echo A game is running
```

`-once` exits `0` when a game is detected, `1` when not, and `2` on errors.

### Flags

| Flag | Meaning |
| --- | --- |
| `-config PATH` | Use a specific config file. |
| `-json` | Emit events and `-once` output as JSON. |
| `-once` | Scan once, print status, exit. |
| `-verbose` | Log candidate processes and library warnings. |
| `-listen ADDR` | Serve `GET /status` and `GET /healthz`, e.g. `127.0.0.1:8732`. |
| `-webhook URL` | POST every `game_started`, `game_stopped`, `gaming_started`, and `gaming_stopped` event as JSON. |
| `-status-file PATH` | Atomically write the current status JSON on every poll. |
| `-version` | Print version and exit. |

## Status API

```bat
gamewatch.exe -listen 127.0.0.1:8732
curl http://127.0.0.1:8732/status?pretty=1
```

```json
{
  "gaming": true,
  "game": {
    "name": "Fortnite",
    "source": "epic",
    "exe": "FortniteClient-Win64-Shipping.exe",
    "pid": 5678,
    "path": "D:\\Epic\\Fortnite\\FortniteGame\\Binaries\\Win64\\FortniteClient-Win64-Shipping.exe",
    "title": "Fortnite",
    "startedAt": "2026-10-27T12:39:55Z",
    "fullscreen": true,
    "foreground": true
  },
  "games": [
    {
      "name": "Fortnite",
      "source": "epic",
      "exe": "FortniteClient-Win64-Shipping.exe",
      "pid": 5678,
      "fullscreen": true,
      "foreground": true
    },
    {
      "name": "ELDEN RING",
      "source": "steam",
      "appId": "1245620",
      "exe": "eldenring.exe",
      "pid": 1234,
      "fullscreen": false,
      "foreground": false
    }
  ],
  "since": "2026-10-27T12:37:41Z",
  "durationSeconds": 141,
  "checkedAt": "2026-10-27T12:40:02Z"
}
```

`game` is the primary game (foreground/fullscreen first), while `games` lists
every game currently being tracked.

The server only binds to the address you pass. Use `127.0.0.1` unless you want
other machines on your network to read it.

## Configuration

Copy `gamewatch.example.json` to `gamewatch.json` next to the executable (or
pass `-config`). Fields omitted from the file keep their defaults.

```json
{
  "pollSeconds": 5,
  "startDelaySeconds": 10,
  "stopDelaySeconds": 20,
  "libraryRefreshMinutes": 60,
  "hookTimeoutSeconds": 30,
  "steam": true,
  "epic": true,
  "gog": true,
  "ubisoft": true,
  "detectUnknownFullscreen": false,
  "ignoredExecutables": [],
  "ignoredGames": [],
  "hooks": {
    "gamingStarted": "C:\\Scripts\\gaming-on.cmd",
    "gameStarted": "python C:\\Scripts\\discord_game.py",
    "gameStopped": "",
    "gamingStopped": "C:\\Scripts\\gaming-off.cmd"
  },
  "games": [
    {
      "name": "My Game",
      "installDir": "D:\\Games\\My Game"
    },
    {
      "name": "Minecraft",
      "executables": ["javaw.exe"],
      "matchWindowTitle": "Minecraft"
    }
  ]
}
```

Custom game matching rules:

- `executables` matches the process file name, case-insensitively.
- `installDir` matches any executable below that directory.
- `matchWindowTitle` requires a case-insensitive substring in the game window
  title. Use it for shared executables such as `javaw.exe`.
- If `installDir` and `executables` are both present, the executable must be in
  the directory.
- Custom entries are checked before launcher and built-in matches, and are not
  filtered by `ignoredExecutables`.

`ignoredExecutables` and `ignoredGames` are both JSON arrays of strings. Each
entry must be its own quoted value, separated by a comma:

```json
{
  "ignoredExecutables": [
    "FortniteLauncher.exe",
    "MyGameHelper.exe"
  ],
  "ignoredGames": [
    "Fortnite",
    "ELDEN RING"
  ]
}
```

Add these fields to your existing `gamewatch.json` object rather than replacing
your other settings.

`ignoredExecutables` is for helper processes (crash handlers, anti-cheat
installers, launchers) that live inside a game folder:

- Use only the executable file name, including `.exe`; do not use a path.
- Matching is case-insensitive and exact. `MyGameHelper.exe` does not match
  `OtherMyGameHelper.exe`.
- It also applies to installed-library and built-in executable matches. It does
  not filter explicit custom games in `games`; those take precedence.

`ignoredGames` suppresses a whole game by name:

- Use the exact game name that gamewatch reports, not the executable name or
  install folder.
- Matching is case-insensitive, but the whole name must match. `Fortnite` does
  not match `Fortnite Festival`.
- The name comes from the launcher manifest, the built-in game list, your custom
  game name, or the window title for unknown fullscreen apps.
- Use `gamewatch.exe -verbose`, the JSON output, or `/status` to see the names
  gamewatch is matching.

### Events

| Event | When it is emitted |
| --- | --- |
| `game_started` | A game has been detected for `startDelaySeconds`. |
| `game_stopped` | A tracked game has not been seen for `stopDelaySeconds`. |
| `gaming_started` | The first game starts; emitted after the poll's `game_started` events. |
| `gaming_stopped` | The last game stops; emitted after the final `game_stopped`. |

`status.gaming` is `true` while at least one game is active. With two games
running, closing one emits `game_stopped` but leaves `gaming` true; closing the
second emits `game_stopped` followed by `gaming_stopped`.

Game events include a `game` object. Gaming-mode events do not, because more
than one game may be active; use `status.games` in the event to see the full
list.

### Hooks

Each hook is a command line run by the platform shell (`cmd.exe /C` on Windows).
Hooks run asynchronously in event order, so a slow script does not block
detection. Output goes to gamewatch's stderr, keeping `-json` output clean.

```json
"hooks": {
  "gamingStarted": "curl -s -X POST http://127.0.0.1:8123/api/services/script/gaming_on",
  "gameStarted": "python C:\\Scripts\\discord_game.py",
  "gameStopped": "",
  "gamingStopped": "curl -s -X POST http://127.0.0.1:8123/api/services/script/gaming_off"
}
```

- `gamingStarted` / `gamingStopped` run when `status.gaming` becomes true/false.
- `gameStarted` / `gameStopped` run for each individual game.
- `hookTimeoutSeconds` (default `30`) kills a hook that runs too long.
- An empty command disables that hook.
- Commands run in the same working directory as gamewatch.

The complete event is written as JSON to the hook's stdin, so a Python script
can do:

```python
import json
import sys

event = json.load(sys.stdin)
print(event["event"], file=sys.stderr)
if event.get("game"):
    print(event["game"]["name"], file=sys.stderr)
print(event["status"]["games"], file=sys.stderr)
```

The same fields are available as environment variables:

| Variable | Meaning |
| --- | --- |
| `GAMEWATCH_EVENT` | Event name, for example `game_started`. |
| `GAMEWATCH_GAMING` | `true` or `false`. |
| `GAMEWATCH_GAME_COUNT` | Number of active games. |
| `GAMEWATCH_GAME_NAME` | Event game name. For mode events, the primary game. |
| `GAMEWATCH_GAME_SOURCE` | `steam`, `epic`, `gog`, `ubisoft`, `known`, `custom`, or `unknown`. |
| `GAMEWATCH_GAME_APPID` | Launcher app ID, when known. |
| `GAMEWATCH_GAME_EXE` | Executable name. |
| `GAMEWATCH_GAME_PID` | Process ID. |
| `GAMEWATCH_GAME_PATH` | Full executable path. |
| `GAMEWATCH_GAME_TITLE` | Window title. |
| `GAMEWATCH_GAME_FULLSCREEN` | `true` or `false`. |
| `GAMEWATCH_GAME_FOREGROUND` | `true` or `false`. |
| `GAMEWATCH_DURATION_SECONDS` | Event duration for stop events. |
| `GAMEWATCH_AT` | Event timestamp in RFC 3339 format. |

`-webhook URL` sends the same event JSON to an HTTP endpoint instead of, or in
addition to, local hooks.

### Unknown fullscreen apps

Set `"detectUnknownFullscreen": true` to report any foreground fullscreen app
that is not a known game. This catches unlisted standalone games, but can also
catch fullscreen video or presentations. Browsers, media players, OBS, Discord,
Zoom and Teams are excluded by default.

## Running at login

Create a Task Scheduler task with:

- Trigger: **At log on**
- Action: start `gamewatch.exe`
- Start in: the folder containing `gamewatch.json`
- Optional arguments: `-listen 127.0.0.1:8732 -status-file "%LOCALAPPDATA%\gamewatch\status.json"`

Do not run the detector elevated unless your games run elevated. Elevation
changes which process paths are readable.

## How detection works (and limits)

- A game is counted when a matched process has a visible top-level window. A
  minimized game still counts; a game that has fully exited does not.
- All matched games are tracked at the same time. Closing one game does not end
  gaming mode while another game is still active.
- The start and stop delays apply per game. Gaming mode starts after the first
  game passes its start delay and stops after the last game passes its stop
  delay.
- Foreground and fullscreen are included in events and used to pick the primary
  game when several are running. A game does not have to be fullscreen to count,
  which supports windowed play.
- Steam `RunningAppID` is used only when no process match was found and
  `steam.exe` is running. This helps when the game process is elevated and its
  path cannot be read.
- There is no injection, hooking, GPU polling or anti-cheat interaction. The
  tool only reads process/window state and launcher metadata.
- UWP / Xbox Game Pass games that render through `ApplicationFrameHost` are not
  auto-detected. Add their real executable via a custom entry.
- Some launchers (Battle.net, EA, Xbox) do not expose an install library. Their
  games are still detected through the built-in executable list or a custom
  `installDir` entry.

## Credits

The approach was informed by these open-source projects:

- [**GameDetect**](https://github.com/opkelde/GameDetect) - Windows game
  detection and Home Assistant status publishing; process polling, D3D
  fullscreen, and foreground-window detection.
- [**GameDetector**](https://github.com/FabioZumbi12/game-detector) - OBS plugin
  for detecting Steam/Epic games; launcher library discovery and game identity.
- [**PlayCounter**](https://github.com/zntr1/PlayCounter) - launcher-agnostic
  playtime tracker; process scanning, launcher manifest parsing, and
  path-based game identity.
- [**Gaming-Status**](https://github.com/adamjthompson/Gaming-Status) - Home
  Assistant integration for aggregated gaming status; session tracking and
  event reporting.

`gamewatch` is an independent implementation and shares no code with these
projects. It uses the same public Windows APIs and launcher file formats.
