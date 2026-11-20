# Azeroth Hub

A desktop control panel for a local **AzerothCore (WoTLK 3.3.5a) server** — one window instead
of three console windows, and the reason it exists: **it stops the stack in the right order**,
which is what keeps your characters from being lost.

Built for the [Firues/azerothcore-wotlk](https://github.com/Firues/azerothcore-wotlk) living-world
repack, but it works with any AzerothCore layout that has `worldserver.exe`, `configs/` and
`data/dbc` in one folder.

---

## 📥 Download

**[Latest release](https://github.com/Firues/azeroth-hub/releases/latest)** — one zip with
`wow-hub.exe` and an install note. No compiling, no runtime dependencies: the frontend is
embedded in the exe.

Every push to `main` is compiled automatically by GitHub Actions; per-commit builds are on the
[Actions page](https://github.com/Firues/azeroth-hub/actions).

## 🚀 Install

1. Copy `wow-hub.exe` into your **repack root** — the folder that contains `worldserver.exe`,
   `configs/` and `data/dbc`. The panel walks up from its own location to find them.
2. Run it. The checklist on the first screen says what is missing, if anything.
3. Backups need [7-Zip](https://www.7-zip.org/) installed (`C:\Program Files\7-Zip\7z.exe`).

## ✨ What it does

| | What it means for you |
|---|---|
| **Correct stop order, always** | Stop = world → authserver → MySQL. `worldserver` writes to the database while it shuts down, so a MySQL that leaves first leaves the world writing into a dead connection — that is the path to losing a character. The panel refuses to stop MySQL while the world is up, no matter which button was pressed. |
| **One window, not three consoles** | Start everything with one button, watch each service's state, memory and PID, start/stop/restart any service individually. |
| **Rollback points** | Two kinds of copy: *without stopping the server* (four readable .sql dumps) and *full* (a 7-Zip snapshot of `mysql\data`, restorable with one button). Progress, cancel, and an outcome box that says where the folder went. Restores stop the stack, take a safety copy of the current state first, and tell you where that safety copy lives. |
| **A backup reminder that counts by the calendar** | Say "every N days" and the panel reminds (or, if you allow it, makes the point itself) — a week stays a week even if the window was never opened. |
| **Health checks that name the fix** | Six checks: repack in place, MySQL process/port/ping, authserver port, world process. Every failure comes with what to do about it, in words. |
| **Characters and accounts browser** | Searchable lists read straight from the realm database — read-only: the account query never touches passwords, salts or session keys. |
| **Folders one click away** | Open `backups\`, `logs\` or the repack root in Explorer from the panel. |
| **Tray icon** | Closing the window asks first while anything is running; the panel comes back from the tray. |

## 🌍 Languages

English, Русский, 简体中文, Español, Português (Brasil), Français, Deutsch — switch on the
**More** screen; the choice is remembered. A Russian Windows starts in Russian, any other
in English. Both sides of the panel translate: the page's own words and every sentence the
backend builds (checklist, health details, errors).

## 🛠 Building from source

Go 1.27, Node 22, no Wails CLI installation needed — the recipe (also in
[AGENTS.md](AGENTS.md), section 4):

```bash
go vet ./... && go test ./...
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27 generate bindings -ts -d ./frontend/bindings
npm --prefix frontend ci
npm --prefix frontend run build
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27 generate icons --input build/appicon.png   # ends in an error after writing the .ico — expected on Windows
go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.27 generate syso -arch amd64 -icon build/windows/icon.ico -manifest build/windows/wails.exe.manifest -info build/windows/info.json -out rsrc_windows_amd64.syso
go build -tags production -trimpath -buildvcs=false -ldflags="-w -s -H windowsgui" -o build/bin/wow-hub.exe .
```

The same steps, in the same order, are what `.github/workflows/build.yml` runs.

## 🙏 Credits

- [AzerothCore](https://github.com/azerothcore/azerothcore-wotlk) and
  [mod-playerbots](https://github.com/mod-playerbots/mod-playerbots) — the server this panel
  drives.
- [Wails v3](https://wails.io), Svelte 5, Tailwind CSS 4, shadcn-svelte.

---

*Personal, non-commercial project. World of Warcraft is a trademark of Blizzard Entertainment;
no affiliation.*
