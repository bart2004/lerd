# Fork Features

This fork extends upstream Lerd with four quality-of-life improvements while keeping all changes modular so merging or rebasing `upstream/main` remains straightforward.

## 1. Native Cross-Platform Desktop App (`desktop/`)

A real Tauri v2 desktop application for Linux, macOS, and Windows (not a browser PWA):

- Dedicated native window with single-instance focus, `lerd://` deep-link routing, and close-to-tray behavior.
- Native system tray populated dynamically from `/api/tray` and `/api/tray/action` across all platforms.
- Native OS notifications streamed over `/api/desktop/ws`.
- Native application menu bar with keyboard shortcuts and **Open in Browser** (`Ctrl/Cmd+B`), plus `lerd dashboard --browser` on the CLI so the browser dashboard stays available anytime.
- Built-in boot screen that starts Lerd with one click when the daemon is stopped.

Build locally:

```bash
make build-desktop
make install-desktop
```

Full cross-platform installers (`.AppImage`/`.deb`, `.dmg`, `.msi`/`.exe`) are built via `.github/workflows/desktop.yml` or `cd desktop/src-tauri && cargo tauri build`.

## 2. Loopback `.env` Hosts (`127.0.0.1`)

Containerized PHP sites can use `127.0.0.1` and each service's published host port in `.env` instead of container DNS names (`lerd-mysql`, `lerd-redis`, etc.), matching the local ergonomics of Laravel Herd while working identically from both the host and inside PHP containers.

- `cmd/lerd-loopback` runs alongside `php-fpm` inside PHP containers and watches `~/.local/share/lerd/run/loopback-ports.json`, forwarding `127.0.0.1:<hostPort>` over the `lerd` bridge to `lerd-<service>:<containerPort>` (skipping port 9000 owned by PHP-FPM).
- Enabled automatically when `lerd-loopback` is installed (`~/.local/share/lerd/bin/lerd-loopback`). Override in `config.yaml` under `php.loopback_hosts: true|false`.

## 3. Host Binaries Inside PHP Containers (`lerd php:host-bin`)

Expose allowlisted host binaries (such as `wkhtmltopdf`) transparently inside PHP containers without rebuilding images or restarting PHP-FPM:

```bash
lerd php:host-bin add wkhtmltopdf
lerd php:host-bin list
lerd php:host-bin remove wkhtmltopdf
```

- `lerd-ui` serves an allowlisted RPC bridge over `~/.local/share/lerd/run/lerd-host-exec.sock` on Linux (and bearer-token authenticated loopback TCP on macOS).
- `cmd/lerd-host-exec` is symlinked into `~/.local/share/lerd/host-bins/<name>` (prepended to container `PATH`) and automatically stages container-local `/tmp` input/output files across the container boundary.

## 4. Installer Conflict Detection & Cleanup (`install.sh`)

`install.sh` checks for conflicting local setups before installing:

- Existing Valet installations (`valet`, `~/.config/valet`, `~/.valet`, Valet dnsmasq/nginx drop-ins)
- Active system services holding ports 80/443/53 (`nginx`, `apache2`/`httpd`, `caddy`, `php*-fpm`, standalone `dnsmasq.service`)
- Live listeners on ports 80/443 (ignoring Lerd's own rootless Podman processes)
- Broken or Tailscale-overwritten `/etc/resolv.conf` (`100.100.100.100`, broken symlinks, or bypassed `systemd-resolved`)

When conflicts are found, the installer lists them and asks whether to clean them up automatically. Non-interactive flags `--cleanup-conflicts` and `--skip-conflict-cleanup` are also supported.

## Keeping Up to Date with Upstream

Fork features live primarily in isolated directories (`desktop/`, `internal/loopback/`, `internal/hostexec/`, `cmd/lerd-loopback/`, `cmd/lerd-host-exec/`, `internal/ui/hostexec.go`, `internal/serviceops/loopback_sync.go`, `internal/cli/php_host_bin.go`) with only minimal hooks in upstream files:

```bash
git fetch upstream
git merge upstream/main
```
