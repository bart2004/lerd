mod commands;
mod config;
mod menu;
mod notify;
mod tray;

use tauri::webview::Color;
use tauri::{AppHandle, Manager, WebviewUrl, WebviewWindowBuilder, WindowEvent, Wry};
use tauri_plugin_deep_link::DeepLinkExt;

pub fn show_main_window(app: &AppHandle<Wry>, route: Option<&str>) {
    if let Some(win) = app.get_webview_window("main") {
        if let Some(r) = route {
            if let Ok(url) = config::dashboard_url_for_route(Some(r)) {
                let _ = win.navigate(url);
            }
        }
        let _ = win.show();
        let _ = win.unminimize();
        let _ = win.set_focus();
    }
}

fn handle_deep_links(app: &AppHandle<Wry>, urls: Vec<String>) {
    for raw in urls {
        if let Some(route) = config::route_from_deep_link(&raw) {
            show_main_window(app, Some(&route));
            return;
        }
    }
    show_main_window(app, None);
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_notification::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_single_instance::init(|app, args, _cwd| {
            let urls: Vec<String> = args
                .into_iter()
                .filter(|a| a.starts_with("lerd://"))
                .collect();
            if urls.is_empty() {
                show_main_window(app, None);
            } else {
                handle_deep_links(app, urls);
            }
        }))
        .invoke_handler(tauri::generate_handler![
            commands::check_backend,
            commands::dashboard_url,
            commands::start_lerd,
            commands::stop_lerd,
            commands::open_in_browser,
        ])
        .setup(|app| {
            let handle = app.handle().clone();

            if let Ok(app_menu) = menu::build_app_menu(&handle) {
                let _ = app.set_menu(app_menu);
            }
            app.on_menu_event(|app, event| {
                menu::handle_menu_event(app, event.id().as_ref());
            });

            let _ = tray::init_tray(&handle);
            notify::spawn_notification_listener(handle.clone());

            let dl_handle = handle.clone();
            app.deep_link().on_open_url(move |event| {
                let urls: Vec<String> = event.urls().into_iter().map(|u| u.to_string()).collect();
                handle_deep_links(&dl_handle, urls);
            });

            let win = WebviewWindowBuilder::new(app, "main", WebviewUrl::App("index.html".into()))
                .title("Lerd")
                .inner_size(1280.0, 820.0)
                .min_inner_size(900.0, 600.0)
                .center()
                .resizable(true)
                .decorations(true)
                .background_color(Color(13, 13, 13, 255))
                .initialization_script("window.__LERD_DESKTOP__ = true;")
                .build()?;

            let nav_win = win.clone();
            tauri::async_runtime::spawn(async move {
                if commands::is_backend_alive().await {
                    if let Ok(url) = config::dashboard_url_for_route(None) {
                        let _ = nav_win.navigate(url);
                    }
                }
            });

            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .run(tauri::generate_context!())
        .expect("error while running Lerd desktop application");
}
