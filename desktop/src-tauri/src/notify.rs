use std::time::Duration;
use futures_util::StreamExt;
use serde::Deserialize;
use tauri::{AppHandle, Wry};
use tauri_plugin_notification::NotificationExt;
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::Message;

use crate::config;

#[derive(Debug, Deserialize)]
struct DesktopNotification {
    #[serde(rename = "type")]
    kind: String,
    title: String,
    #[serde(default)]
    body: String,
}

pub fn spawn_notification_listener(app: AppHandle<Wry>) {
    tauri::async_runtime::spawn(async move {
        let mut backoff = Duration::from_secs(2);
        loop {
            let ws_url = config::backend_ws_url();
            match connect_async(&ws_url).await {
                Ok((mut ws, _)) => {
                    backoff = Duration::from_secs(2);
                    while let Some(msg) = ws.next().await {
                        match msg {
                            Ok(Message::Text(text)) => {
                                if let Ok(n) = serde_json::from_str::<DesktopNotification>(&text) {
                                    if n.kind == "notification" && !n.title.is_empty() {
                                        let mut builder = app.notification().builder().title(&n.title);
                                        if !n.body.is_empty() {
                                            builder = builder.body(&n.body);
                                        }
                                        let _ = builder.show();
                                    }
                                }
                            }
                            Ok(Message::Close(_)) | Err(_) => break,
                            _ => {}
                        }
                    }
                }
                Err(_) => {}
            }
            tokio::time::sleep(backoff).await;
            backoff = (backoff * 2).min(Duration::from_secs(15));
        }
    });
}
