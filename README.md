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
produce start/stop spam.

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
13:10:02 game stopped: ELDEN RING after 32m21s
```

Use `-json` for one JSON event per line:

```bat
gamewatch.exe -json
```

```json
{"event":"game_started","at":"2026-10-27T12:37:41Z","game":{"name":"ELDEN RING","source":"steam","appId":"1245620","exe":"eldenring.exe","pid":1234,"fullscreen":true,"foreground":true},"status":{"gaming":true,"durationSeconds":0}}
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
| `-webhook URL` | POST each `game_started` / `game_stopped` event as JSON. |
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
    "name": "ELDEN RING",
    "source": "steam",
    "appId": "1245620",
    "exe": "eldenring.exe",
    "pid": 1234,
    "path": "D:\\SteamLibrary\\steamapps\\common\\ELDEN RING\\Game\\eldenring.exe",
    "title": "ELDEN RING",
    "startedAt": "2026-10-27T12:36:10Z",
    "fullscreen": true,
    "foreground": true
  },
  "since": "2026-10-27T12:37:41Z",
  "durationSeconds": 141,
  "checkedAt": "2026-10-27T12:40:02Z"
}
```

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
  "steam": true,
  "epic": true,
  "gog": true,
  "ubisoft": true,
  "detectUnknownFullscreen": false,
  "ignoredExecutables": [],
  "ignoredGames": [],
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

`ignoredExecutables` is for helper processes (crash handlers, anti-cheat
installers, launchers) that live inside a game folder. `ignoredGames` suppresses
a whole game by name.

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
- Foreground and fullscreen are included in events and used to pick between
  multiple running games. A game does not have to be fullscreen to count, which
  supports windowed play.
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

The approach was informed by the example projects in this repository:

- **GameDetect** - process polling, D3D fullscreen and foreground-window
  detection.
- **GameDetector** (OBS plugin) - Steam/Epic/GOG/Ubisoft library discovery.
- **PlayCounter** - launcher manifest parsing and path-based game identity.

No code is shared at runtime; these are independent implementations of the same
public Windows APIs and launcher file formats.
