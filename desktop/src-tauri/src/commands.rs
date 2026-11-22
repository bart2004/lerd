use std::path::PathBuf;
use std::time::Duration;
use tauri::AppHandle;
use tauri_plugin_opener::OpenerExt;
use tokio::process::Command;

use crate::config;

fn resolve_lerd_binary() -> (String, Vec<String>) {
    #[cfg(target_os = "windows")]
    {
        return ("wsl.exe".to_string(), vec!["lerd".to_string()]);
    }

    #[cfg(not(target_os = "windows"))]
    {
        let mut candidates: Vec<PathBuf> = Vec::new();
        if let Ok(home) = std::env::var("HOME") {
            candidates.push(PathBuf::from(&home).join(".local/bin/lerd"));
        }
        candidates.push(PathBuf::from("/opt/homebrew/bin/lerd"));
        candidates.push(PathBuf::from("/usr/local/bin/lerd"));
        candidates.push(PathBuf::from("/usr/bin/lerd"));

        for path in candidates {
            if path.is_file() {
                return (path.to_string_lossy().to_string(), Vec::new());
            }
        }
        ("lerd".to_string(), Vec::new())
    }
}

pub async fn run_lerd_cli(args: &[&str]) -> Result<String, String> {
    let (bin, mut prefix) = resolve_lerd_binary();
    for a in args {
        prefix.push((*a).to_string());
    }
    let output = Command::new(&bin)
        .args(&prefix)
        .output()
        .await
        .map_err(|e| format!("failed to run {bin}: {e}"))?;

    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        let stdout = String::from_utf8_lossy(&output.stdout).trim().to_string();
        let msg = if !stderr.is_empty() { stderr } else { stdout };
        Err(if msg.is_empty() {
            format!("{bin} exited with {}", output.status)
        } else {
            msg
        })
    }
}

pub async fn is_backend_alive() -> bool {
    let client = match reqwest::Client::builder()
        .timeout(Duration::from_millis(800))
        .build()
    {
        Ok(c) => c,
        Err(_) => return false,
    };
    let url = format!("{}/api/status", config::backend_base_url());
    matches!(client.get(url).send().await, Ok(resp) if resp.status().is_success())
}

#[tauri::command]
pub async fn check_backend() -> bool {
    is_backend_alive().await
}

#[tauri::command]
pub fn dashboard_url() -> String {
    format!("{}/", config::backend_base_url())
}

#[tauri::command]
pub async fn start_lerd() -> Result<String, String> {
    run_lerd_cli(&["start"]).await
}

#[tauri::command]
pub async fn stop_lerd() -> Result<String, String> {
    run_lerd_cli(&["stop"]).await
}

#[tauri::command]
pub fn open_in_browser(app: AppHandle, route: Option<String>) -> Result<(), String> {
    let url = config::browser_url_for_route(route.as_deref());
    app.opener()
        .open_url(url, None::<&str>)
        .map_err(|e| e.to_string())
}
