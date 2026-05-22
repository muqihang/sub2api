use std::env;
use std::process::Command;
use std::process::Stdio;
use std::thread;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tauri::menu::{MenuBuilder, MenuItemBuilder};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::{Emitter, Manager};

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct SidecarRequest {
    args: Vec<String>,
    timeout_ms: Option<u64>,
}

#[derive(Debug, Serialize)]
struct SidecarFailure {
    ok: bool,
    status: &'static str,
    error: SidecarErrorBody,
}

#[derive(Debug, Serialize)]
struct SidecarErrorBody {
    code: &'static str,
    message: String,
}

#[tauri::command]
fn run_sidecar(args: Vec<String>, timeout_ms: Option<u64>) -> Result<Value, Value> {
    let request = SidecarRequest { args, timeout_ms };
    validate_sidecar_request(&request).map_err(|error| json!(error))?;
    run_sidecar_command(request).map_err(|error| json!(error))
}

fn validate_sidecar_request(request: &SidecarRequest) -> Result<(), SidecarFailure> {
    let uses_desktop_command = request.args.first().map(String::as_str) == Some("desktop");
    let requests_json = request.args.iter().any(|arg| arg == "--json");
    if uses_desktop_command && requests_json {
        return Ok(());
    }
    Err(sidecar_failure(
        "invalid_sidecar_command",
        "desktop shell may only call `zhumeng-agent desktop ... --json`".to_string(),
    ))
}

fn run_sidecar_command(request: SidecarRequest) -> Result<Value, SidecarFailure> {
    let executable = env::var("ZHUMENG_AGENT_BIN").unwrap_or_else(|_| "zhumeng-agent".to_string());
    run_sidecar_command_with_executable(&executable, request)
}

fn run_sidecar_command_with_executable(
    executable: &str,
    request: SidecarRequest,
) -> Result<Value, SidecarFailure> {
    let timeout = Duration::from_millis(request.timeout_ms.unwrap_or(5_000));
    let mut child = Command::new(executable)
        .args(&request.args)
        .env("ZHUMENG_DESKTOP_TIMEOUT_MS", timeout.as_millis().to_string())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|error| sidecar_failure("spawn_failed", format!("failed to run zhumeng-agent: {error}")))?;

    let started_at = Instant::now();
    let output = loop {
        match child.try_wait() {
            Ok(Some(_status)) => {
                break child
                    .wait_with_output()
                    .map_err(|error| sidecar_failure("wait_failed", format!("failed to collect sidecar output: {error}")))?;
            }
            Ok(None) if started_at.elapsed() >= timeout => {
                let _ = child.kill();
                let output = child.wait_with_output().ok();
                let stderr = output
                    .as_ref()
                    .map(|value| String::from_utf8_lossy(&value.stderr).to_string())
                    .unwrap_or_default();
                return Err(sidecar_failure(
                    "timeout",
                    format!("sidecar timed out after {}ms; stderr={}", timeout.as_millis(), redact_for_log(&stderr)),
                ));
            }
            Ok(None) => thread::sleep(Duration::from_millis(20)),
            Err(error) => {
                return Err(sidecar_failure(
                    "wait_failed",
                    format!("failed while waiting for sidecar: {error}"),
                ))
            }
        }
    };

    let stdout = String::from_utf8_lossy(&output.stdout);
    let stderr = String::from_utf8_lossy(&output.stderr);
    let parsed: Value = serde_json::from_str(stdout.trim()).map_err(|error| {
        sidecar_failure(
            "invalid_json",
            format!("sidecar returned non-json stdout: {error}; stderr={}", redact_for_log(&stderr)),
        )
    })?;

    if output.status.success() || parsed.get("ok").and_then(Value::as_bool) == Some(false) {
        Ok(parsed)
    } else {
        Err(sidecar_failure(
            "exit_failed",
            format!(
                "sidecar exited with {}; stderr={}",
                output.status.code().map_or_else(|| "signal".to_string(), |code| code.to_string()),
                redact_for_log(&stderr)
            ),
        ))
    }
}

fn sidecar_failure(code: &'static str, message: String) -> SidecarFailure {
    SidecarFailure {
        ok: false,
        status: "error",
        error: SidecarErrorBody { code, message },
    }
}

fn redact_for_log(text: &str) -> String {
    text.split_whitespace()
        .map(|part| {
            let lower = part.to_ascii_lowercase();
            if lower.contains("token") || lower.contains("secret") || lower.contains("authorization") {
                "<redacted>"
            } else {
                part
            }
        })
        .collect::<Vec<_>>()
        .join(" ")
}

fn focus_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn install_tray(app: &tauri::App) -> tauri::Result<()> {
    let open = MenuItemBuilder::with_id("open", "打开逐梦注入工具").build(app)?;
    let repair = MenuItemBuilder::with_id("repair-codex", "修复 Codex 接入").build(app)?;
    let open_codex = MenuItemBuilder::with_id("open-codex", "打开 Codex App").build(app)?;
    let quit = MenuItemBuilder::with_id("quit", "退出").build(app)?;
    let menu = MenuBuilder::new(app)
        .items(&[&open, &repair, &open_codex, &quit])
        .build()?;

    TrayIconBuilder::with_id("zhumeng-agent")
        .tooltip("逐梦注入工具")
        .menu(&menu)
        .on_menu_event(|app, event| match event.id().as_ref() {
            "open" => focus_main_window(app),
            "repair-codex" => {
                focus_main_window(app);
                let _ = app.emit("tray-action", "repair-codex");
            }
            "open-codex" => {
                focus_main_window(app);
                let _ = app.emit("tray-action", "open-codex");
            }
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|tray, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                focus_main_window(tray.app_handle());
            }
        })
        .build(app)?;
    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, argv, _cwd| {
            focus_main_window(app);
            let urls: Vec<String> = argv
                .into_iter()
                .filter(|arg| arg.starts_with("zhumeng-agent://"))
                .collect();
            if !urls.is_empty() {
                let _ = app.emit("deep-link", urls);
            }
        }))
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![run_sidecar])
        .setup(|app| {
            install_tray(app)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running zhumeng agent desktop");
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn sidecar_command_parses_json_stdout() {
        let value = run_sidecar_command_with_executable(
            "/bin/sh",
            SidecarRequest {
                args: vec![
                    "-c".to_string(),
                    "printf '{\"ok\":true,\"status\":\"ok\",\"data\":{\"x\":1}}'".to_string(),
                ],
                timeout_ms: Some(1_000),
            },
        )
        .expect("sidecar json should parse");

        assert_eq!(value["data"]["x"], 1);
    }

    #[test]
    fn sidecar_command_redacts_invalid_json_stderr() {
        let error = run_sidecar_command_with_executable(
            "/bin/sh",
            SidecarRequest {
                args: vec![
                    "-c".to_string(),
                    "printf 'not-json'; printf ' access_token=secret-token-value' >&2".to_string(),
                ],
                timeout_ms: Some(1_000),
            },
        )
        .expect_err("invalid json should fail");
        let dumped = serde_json::to_string(&error).expect("error should serialize");

        assert!(dumped.contains("<redacted>"));
        assert!(!dumped.contains("secret-token-value"));
    }

    #[test]
    fn sidecar_command_times_out() {
        let error = run_sidecar_command_with_executable(
            "/bin/sh",
            SidecarRequest {
                args: vec!["-c".to_string(), "sleep 1".to_string()],
                timeout_ms: Some(20),
            },
        )
        .expect_err("timeout should fail");

        assert_eq!(error.error.code, "timeout");
    }

    #[test]
    fn sidecar_request_requires_desktop_json_contract() {
        validate_sidecar_request(&SidecarRequest {
            args: vec!["desktop".to_string(), "status".to_string(), "--json".to_string()],
            timeout_ms: None,
        })
        .expect("desktop json commands are allowed");

        let error = validate_sidecar_request(&SidecarRequest {
            args: vec!["doctor".to_string(), "--json".to_string()],
            timeout_ms: None,
        })
        .expect_err("non-desktop commands are rejected");
        assert_eq!(error.error.code, "invalid_sidecar_command");

        let error = validate_sidecar_request(&SidecarRequest {
            args: vec!["desktop".to_string(), "status".to_string()],
            timeout_ms: None,
        })
        .expect_err("desktop commands must ask for json");
        assert_eq!(error.error.code, "invalid_sidecar_command");
    }
}
