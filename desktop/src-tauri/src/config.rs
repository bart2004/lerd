use url::Url;

pub const DEFAULT_UI_URL: &str = "http://127.0.0.1:7073";
pub const DEFAULT_BROWSER_URL: &str = "http://lerd.localhost";

pub fn backend_base_url() -> String {
    std::env::var("LERD_UI_URL")
        .ok()
        .map(|s| s.trim().trim_end_matches('/').to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| DEFAULT_UI_URL.to_string())
}

pub fn backend_ws_url() -> String {
    let base = backend_base_url();
    if let Some(rest) = base.strip_prefix("https://") {
        format!("wss://{rest}/api/desktop/ws")
    } else if let Some(rest) = base.strip_prefix("http://") {
        format!("ws://{rest}/api/desktop/ws")
    } else {
        format!("ws://{base}/api/desktop/ws")
    }
}

pub fn dashboard_url_for_route(route: Option<&str>) -> Result<Url, url::ParseError> {
    let base = backend_base_url();
    let trimmed = route.unwrap_or("").trim().trim_start_matches('/').trim_start_matches('#').trim_start_matches('/');
    let full = if trimmed.is_empty() {
        format!("{base}/")
    } else {
        format!("{base}/#/{trimmed}")
    };
    Url::parse(&full)
}

pub fn browser_url_for_route(route: Option<&str>) -> String {
    let base = std::env::var("LERD_BROWSER_URL")
        .ok()
        .map(|s| s.trim().trim_end_matches('/').to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| DEFAULT_BROWSER_URL.to_string());
    let trimmed = route.unwrap_or("").trim().trim_start_matches('/').trim_start_matches('#').trim_start_matches('/');
    if trimmed.is_empty() {
        format!("{base}/")
    } else {
        format!("{base}/#/{trimmed}")
    }
}

/// Extracts a dashboard route from a `lerd://...` deep link.
pub fn route_from_deep_link(raw: &str) -> Option<String> {
    let trimmed = raw.trim();
    let rest = trimmed.strip_prefix("lerd://")?;
    let cleaned = rest.trim_start_matches('/').trim_start_matches('#').trim_start_matches('/');
    Some(cleaned.to_string())
}

/// Reports whether a URL belongs to the embedded desktop shell or local Lerd UI
/// backend and should stay inside the desktop window rather than opening in the
/// system browser.
pub fn is_internal_dashboard_url(url: &Url) -> bool {
    match url.scheme() {
        "tauri" | "about" | "blob" | "data" => return true,
        "http" | "https" => {}
        _ => return false,
    }
    if matches!(url.host_str(), Some("tauri.localhost")) {
        return true;
    }
    if let Ok(base) = Url::parse(&backend_base_url()) {
        return url.scheme() == base.scheme()
            && url.host_str() == base.host_str()
            && url.port_or_known_default() == base.port_or_known_default();
    }
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn deep_link_route_parsing() {
        assert_eq!(route_from_deep_link("lerd://"), Some(String::new()));
        assert_eq!(route_from_deep_link("lerd://sites/myapp"), Some("sites/myapp".to_string()));
        assert_eq!(route_from_deep_link("lerd://#/services/mysql"), Some("services/mysql".to_string()));
        assert_eq!(route_from_deep_link("https://example.com"), None);
    }

    #[test]
    fn dashboard_and_browser_urls() {
        let u = dashboard_url_for_route(Some("sites/demo")).unwrap();
        assert_eq!(u.as_str(), "http://127.0.0.1:7073/#/sites/demo");
        assert_eq!(browser_url_for_route(None), "http://lerd.localhost/");
        assert_eq!(browser_url_for_route(Some("/services/redis")), "http://lerd.localhost/#/services/redis");
    }

    #[test]
    fn internal_vs_external_urls() {
        assert!(is_internal_dashboard_url(&Url::parse("http://127.0.0.1:7073/#/sites").unwrap()));
        assert!(is_internal_dashboard_url(&Url::parse("tauri://localhost/index.html").unwrap()));
        assert!(is_internal_dashboard_url(&Url::parse("http://tauri.localhost/index.html").unwrap()));
        assert!(!is_internal_dashboard_url(&Url::parse("http://myapp.test/").unwrap()));
        assert!(!is_internal_dashboard_url(&Url::parse("https://myapp.test/dashboard").unwrap()));
        assert!(!is_internal_dashboard_url(&Url::parse("http://192.168.1.126:9100/").unwrap()));
        assert!(!is_internal_dashboard_url(&Url::parse("https://lerd.sh/docs").unwrap()));
    }
}
