use tauri::menu::{Menu, MenuBuilder, MenuItemBuilder, PredefinedMenuItem, SubmenuBuilder};
use tauri::{AppHandle, Manager, Wry};

use crate::{commands, config, show_main_window};

pub fn build_app_menu(app: &AppHandle<Wry>) -> tauri::Result<Menu<Wry>> {
    let open_browser = MenuItemBuilder::with_id("menu_open_browser", "Open in Browser")
        .accelerator("CmdOrCtrl+B")
        .build(app)?;
    let start_lerd = MenuItemBuilder::with_id("menu_start_lerd", "Start Lerd").build(app)?;
    let stop_lerd = MenuItemBuilder::with_id("menu_stop_lerd", "Stop Lerd").build(app)?;
    let hide_window = MenuItemBuilder::with_id("menu_hide_window", "Hide to Tray")
        .accelerator("CmdOrCtrl+W")
        .build(app)?;
    let quit_app = MenuItemBuilder::with_id("menu_quit", "Quit Lerd")
        .accelerator("CmdOrCtrl+Q")
        .build(app)?;

    let app_submenu = SubmenuBuilder::new(app, "Lerd")
        .item(&open_browser)
        .separator()
        .item(&start_lerd)
        .item(&stop_lerd)
        .separator()
        .item(&hide_window)
        .item(&quit_app)
        .build()?;

    let edit_submenu = SubmenuBuilder::new(app, "Edit")
        .item(&PredefinedMenuItem::undo(app, None)?)
        .item(&PredefinedMenuItem::redo(app, None)?)
        .separator()
        .item(&PredefinedMenuItem::cut(app, None)?)
        .item(&PredefinedMenuItem::copy(app, None)?)
        .item(&PredefinedMenuItem::paste(app, None)?)
        .item(&PredefinedMenuItem::select_all(app, None)?)
        .build()?;

    let nav_dashboard = MenuItemBuilder::with_id("nav_dashboard", "Dashboard")
        .accelerator("CmdOrCtrl+1")
        .build(app)?;
    let nav_sites = MenuItemBuilder::with_id("nav_sites", "Sites")
        .accelerator("CmdOrCtrl+2")
        .build(app)?;
    let nav_services = MenuItemBuilder::with_id("nav_services", "Services")
        .accelerator("CmdOrCtrl+3")
        .build(app)?;
    let nav_system = MenuItemBuilder::with_id("nav_system", "System")
        .accelerator("CmdOrCtrl+4")
        .build(app)?;
    let nav_plugins = MenuItemBuilder::with_id("nav_plugins", "Plugins")
        .accelerator("CmdOrCtrl+5")
        .build(app)?;
    let nav_reload = MenuItemBuilder::with_id("nav_reload", "Reload")
        .accelerator("CmdOrCtrl+R")
        .build(app)?;

    let view_submenu = SubmenuBuilder::new(app, "View")
        .item(&nav_dashboard)
        .item(&nav_sites)
        .item(&nav_services)
        .item(&nav_system)
        .item(&nav_plugins)
        .separator()
        .item(&nav_reload)
        .build()?;

    MenuBuilder::new(app)
        .item(&app_submenu)
        .item(&edit_submenu)
        .item(&view_submenu)
        .build()
}

pub fn handle_menu_event(app: &AppHandle<Wry>, id: &str) {
    match id {
        "menu_open_browser" => {
            let _ = commands::open_in_browser(app.clone(), None);
        }
        "menu_start_lerd" => {
            tauri::async_runtime::spawn(async {
                let _ = commands::run_lerd_cli(&["start"]).await;
            });
        }
        "menu_stop_lerd" => {
            tauri::async_runtime::spawn(async {
                let _ = commands::run_lerd_cli(&["stop"]).await;
            });
        }
        "menu_hide_window" => {
            if let Some(win) = app.get_webview_window("main") {
                let _ = win.hide();
            }
        }
        "menu_quit" => {
            app.exit(0);
        }
        "nav_dashboard" => show_main_window(app, Some("")),
        "nav_sites" => show_main_window(app, Some("sites")),
        "nav_services" => show_main_window(app, Some("services")),
        "nav_system" => show_main_window(app, Some("system")),
        "nav_plugins" => show_main_window(app, Some("plugins")),
        "nav_reload" => {
            if let Some(win) = app.get_webview_window("main") {
                if let Ok(url) = config::dashboard_url_for_route(None) {
                    let _ = win.navigate(url);
                }
            }
        }
        _ => {}
    }
}
