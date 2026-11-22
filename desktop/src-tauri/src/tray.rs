use std::time::Duration;
use serde::Deserialize;
use tauri::image::Image;
use tauri::menu::{CheckMenuItemBuilder, Menu, MenuBuilder, MenuItemBuilder, SubmenuBuilder};
use tauri::tray::{MouseButton, MouseButtonState, TrayIcon, TrayIconBuilder, TrayIconEvent};
use tauri::{AppHandle, Manager, Wry};
use tauri_plugin_notification::NotificationExt;

use crate::{commands, config, show_main_window};

const TRAY_ID: &str = "lerd-tray";

#[derive(Debug, Deserialize)]
struct ApiTrayMenu {
    #[serde(default)]
    running: bool,
    #[serde(default)]
    items: Vec<ApiTrayItem>,
}

#[derive(Debug, Deserialize)]
struct ApiTrayItem {
    #[serde(default)]
    label: String,
    #[serde(default)]
    detail: String,
    #[serde(default)]
    checked: Option<bool>,
    #[serde(default)]
    action: String,
    #[serde(default)]
    separator: bool,
    #[serde(default)]
    items: Vec<ApiTrayItem>,
}

#[derive(Debug, Deserialize)]
struct TrayActionResult {
    #[serde(default)]
    title: String,
    #[serde(default)]
    body: String,
}

fn format_label(item: &ApiTrayItem) -> String {
    if item.detail.is_empty() {
        item.label.clone()
    } else {
        format!("{} — {}", item.label, item.detail)
    }
}

fn build_submenu(
    app: &AppHandle<Wry>,
    title: &str,
    items: &[ApiTrayItem],
    seq: &mut usize,
) -> tauri::Result<tauri::menu::Submenu<Wry>> {
    let mut sub = SubmenuBuilder::new(app, title);
    for item in items {
        if item.separator {
            sub = sub.separator();
            continue;
        }
        if !item.items.is_empty() {
            let nested = build_submenu(app, &format_label(item), &item.items, seq)?;
            sub = sub.item(&nested);
            continue;
        }
        let label = format_label(item);
        if let Some(checked) = item.checked {
            let id = if item.action.is_empty() {
                *seq += 1;
                format!("info_{}", *seq)
            } else {
                format!("api:{}", item.action)
            };
            let entry = CheckMenuItemBuilder::with_id(id, label)
                .checked(checked)
                .enabled(!item.action.is_empty())
                .build(app)?;
            sub = sub.item(&entry);
        } else {
            let id = if item.action.is_empty() {
                *seq += 1;
                format!("info_{}", *seq)
            } else {
                format!("api:{}", item.action)
            };
            let entry = MenuItemBuilder::with_id(id, label)
                .enabled(!item.action.is_empty())
                .build(app)?;
            sub = sub.item(&entry);
        }
    }
    sub.build()
}

fn build_tray_menu(app: &AppHandle<Wry>, remote: Option<&ApiTrayMenu>) -> tauri::Result<Menu<Wry>> {
    let mut builder = MenuBuilder::new(app);
    let open_win = MenuItemBuilder::with_id("tray_open_window", "Open Lerd").build(app)?;
    let open_browser = MenuItemBuilder::with_id("tray_open_browser", "Open in Browser").build(app)?;
    builder = builder.item(&open_win).item(&open_browser).separator();

    let mut seq = 0usize;
    if let Some(menu) = remote {
        for item in &menu.items {
            if item.separator {
                builder = builder.separator();
                continue;
            }
            if !item.items.is_empty() {
                let sub = build_submenu(app, &format_label(item), &item.items, &mut seq)?;
                builder = builder.item(&sub);
                continue;
            }
            let label = format_label(item);
            if let Some(checked) = item.checked {
                let id = if item.action.is_empty() {
                    seq += 1;
                    format!("info_{seq}")
                } else {
                    format!("api:{}", item.action)
                };
                let entry = CheckMenuItemBuilder::with_id(id, label)
                    .checked(checked)
                    .enabled(!item.action.is_empty())
                    .build(app)?;
                builder = builder.item(&entry);
            } else {
                let id = if item.action.is_empty() {
                    seq += 1;
                    format!("info_{seq}")
                } else {
                    format!("api:{}", item.action)
                };
                let entry = MenuItemBuilder::with_id(id, label)
                    .enabled(!item.action.is_empty())
                    .build(app)?;
                builder = builder.item(&entry);
            }
        }
    } else {
        let offline = MenuItemBuilder::with_id("tray_offline", "Lerd is stopped")
            .enabled(false)
            .build(app)?;
        let start = MenuItemBuilder::with_id("tray_start_lerd", "Start Lerd").build(app)?;
        builder = builder.item(&offline).item(&start);
    }

    let quit = MenuItemBuilder::with_id("tray_quit", "Quit Lerd Desktop").build(app)?;
    builder.separator().item(&quit).build()
}

pub fn init_tray(app: &AppHandle<Wry>) -> tauri::Result<TrayIcon<Wry>> {
    let icon = Image::from_bytes(include_bytes!("../icons/tray.png"))?;
    let initial_menu = build_tray_menu(app, None)?;

    let tray = TrayIconBuilder::with_id(TRAY_ID)
        .icon(icon)
        .tooltip("Lerd")
        .menu(&initial_menu)
        .show_menu_on_left_click(false)
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                let app = tray.app_handle();
                if let Some(win) = app.get_webview_window("main") {
                    if win.is_visible().unwrap_or(false) {
                        let _ = win.hide();
                    } else {
                        show_main_window(app, None);
                    }
                }
            }
        })
        .on_menu_event(|app, event| {
            let id = event.id().as_ref().to_string();
            handle_tray_action(app.clone(), id);
        })
        .build(app)?;

    let handle = app.clone();
    tauri::async_runtime::spawn(async move {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(3))
            .build()
            .unwrap_or_default();
        loop {
            refresh_tray_once(&handle, &client).await;
            tokio::time::sleep(Duration::from_secs(15)).await;
        }
    });

    Ok(tray)
}

async fn refresh_tray_once(app: &AppHandle<Wry>, client: &reqwest::Client) {
    let url = format!("{}/api/tray", config::backend_base_url());
    let remote = match client.get(url).send().await {
        Ok(resp) if resp.status().is_success() => resp.json::<ApiTrayMenu>().await.ok(),
        _ => None,
    };
    if let Some(tray) = app.tray_by_id(TRAY_ID) {
        let tip = match &remote {
            Some(m) if m.running => "Lerd — Running",
            Some(_) => "Lerd — Stopped",
            None => "Lerd — Offline",
        };
        let _ = tray.set_tooltip(Some(tip));
        if let Ok(menu) = build_tray_menu(app, remote.as_ref()) {
            let _ = tray.set_menu(Some(menu));
        }
    }
}

fn handle_tray_action(app: AppHandle<Wry>, id: String) {
    if id == "tray_open_window" {
        show_main_window(&app, None);
        return;
    }
    if id == "tray_open_browser" {
        let _ = commands::open_in_browser(app, None);
        return;
    }
    if id == "tray_quit" {
        app.exit(0);
        return;
    }
    if id == "tray_start_lerd" {
        tauri::async_runtime::spawn(async move {
            let _ = commands::run_lerd_cli(&["start"]).await;
            let client = reqwest::Client::new();
            refresh_tray_once(&app, &client).await;
        });
        return;
    }
    if let Some(action) = id.strip_prefix("api:") {
        let action = action.to_string();
        tauri::async_runtime::spawn(async move {
            let client = reqwest::Client::builder()
                .timeout(Duration::from_secs(30))
                .build()
                .unwrap_or_default();
            let url = format!("{}/api/tray/action", config::backend_base_url());
            let resp = client
                .post(url)
                .header("X-Lerd-CSRF", "1")
                .json(&serde_json::json!({ "action": action }))
                .send()
                .await;
            if let Ok(r) = resp {
                if let Ok(res) = r.json::<TrayActionResult>().await {
                    if !res.title.is_empty() {
                        let mut builder = app.notification().builder().title(&res.title);
                        if !res.body.is_empty() {
                            builder = builder.body(&res.body);
                        }
                        let _ = builder.show();
                    }
                }
            }
            refresh_tray_once(&app, &client).await;
        });
    }
}
