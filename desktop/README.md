# Lerd Desktop

Native cross-platform desktop application for Lerd (Linux, macOS, Windows), built with Tauri v2.

## Features

- Dedicated native application window (not a browser PWA) with single-instance focus and `lerd://` deep-link support.
- Native system tray powered by Lerd's `/api/tray` and `/api/tray/action` endpoints, with close-to-tray behavior.
- Native desktop notifications streamed from `/api/desktop/ws`.
- Native application menu bar with keyboard shortcuts and **Open in Browser** (`Ctrl/Cmd+B`) so the browser dashboard remains one click away.
- Built-in boot screen that starts Lerd with one click when the daemon is stopped.

## Building locally

```bash
# Development
cd desktop/src-tauri && cargo run

# Release binary
make build-desktop

# Full native installers (AppImage/deb on Linux, dmg/app on macOS, msi/nsis on Windows)
cd desktop/src-tauri && cargo tauri build
```
