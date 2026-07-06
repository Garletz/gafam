use std::process::Stdio;
use std::sync::Arc;
use tokio::io::{AsyncReadExt, BufReader};
use tokio::process::Command;
use tokio::sync::{mpsc, RwLock};
use tokio::net::TcpStream;
use serde::{Deserialize, Serialize};
use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::{connect_async_tls_with_config, Connector, tungstenite::Message};
use native_tls::TlsConnector;

// ============================================================
// === SCRCPY BRIDGE MODULE (Manifest 14) ===
// ============================================================

/// Message type bytes matching vpc-relay/scrcpy_hub.go
const MSG_TYPE_VIDEO: u8 = 0x01;
const MSG_TYPE_INPUT: u8 = 0x02;
const MSG_TYPE_DEVICE_INFO: u8 = 0x03;
const MSG_TYPE_SHELL: u8 = 0x04;
const _MSG_TYPE_HEARTBEAT: u8 = 0x05;

/// Scrcpy server version to use
const SCRCPY_SERVER_VERSION: &str = "2.7";

/// Default local port for scrcpy tunnel
const SCRCPY_LOCAL_PORT: u16 = 27183;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AdbDevice {
    pub serial: String,
    pub model: String,
    pub state: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BridgeStatus {
    pub active: bool,
    pub device: Option<String>,
    pub vpc_connected: bool,
    pub uptime_secs: u64,
    pub frames_sent: u64,
}

/// Global bridge state shared across Tauri commands
pub struct BridgeState {
    pub active: bool,
    pub device_serial: Option<String>,
    pub vpc_connected: bool,
    pub started_at: Option<std::time::Instant>,
    pub frames_sent: u64,
    pub shutdown_tx: Option<mpsc::Sender<()>>,
}

impl Default for BridgeState {
    fn default() -> Self {
        Self {
            active: false,
            device_serial: None,
            vpc_connected: false,
            started_at: None,
            frames_sent: 0,
            shutdown_tx: None,
        }
    }
}

pub type SharedBridgeState = Arc<RwLock<BridgeState>>;

/// Find the ADB binary path
fn find_adb() -> String {
    // Check common locations
    let candidates = vec![
        "adb".to_string(),
        // macOS: Android Studio default
        format!("{}/Library/Android/sdk/platform-tools/adb", std::env::var("HOME").unwrap_or_default()),
        // Linux
        "/usr/bin/adb".to_string(),
        "/usr/local/bin/adb".to_string(),
    ];

    for candidate in &candidates {
        if let Ok(output) = std::process::Command::new(candidate).arg("version").output() {
            if output.status.success() {
                return candidate.clone();
            }
        }
    }

    "adb".to_string() // Fallback, will error at runtime if not found
}

/// List connected ADB devices
pub async fn list_devices() -> Result<Vec<AdbDevice>, String> {
    let adb = find_adb();
    let output = Command::new(&adb)
        .args(["devices", "-l"])
        .output()
        .await
        .map_err(|e| format!("Failed to run adb: {}. Make sure ADB is installed.", e))?;

    if !output.status.success() {
        return Err(format!("adb devices failed: {}", String::from_utf8_lossy(&output.stderr)));
    }

    let stdout = String::from_utf8_lossy(&output.stdout);
    let mut devices = Vec::new();

    for line in stdout.lines().skip(1) {
        let line = line.trim();
        if line.is_empty() || line.starts_with('*') {
            continue;
        }

        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() >= 2 {
            let serial = parts[0].to_string();
            let state = parts[1].to_string();

            // Extract model name from properties
            let mut model = serial.clone();
            for part in &parts[2..] {
                if part.starts_with("model:") {
                    model = part.trim_start_matches("model:").to_string();
                    break;
                }
            }

            devices.push(AdbDevice {
                serial,
                model,
                state,
            });
        }
    }

    Ok(devices)
}

/// Connect to a device via WiFi ADB
pub async fn connect_wifi(ip: &str) -> Result<String, String> {
    let adb = find_adb();
    let target = if ip.contains(':') { ip.to_string() } else { format!("{}:5555", ip) };

    let output = Command::new(&adb)
        .args(["connect", &target])
        .output()
        .await
        .map_err(|e| format!("adb connect failed: {}", e))?;

    let result = String::from_utf8_lossy(&output.stdout).to_string();
    if result.contains("connected") {
        Ok(result)
    } else {
        Err(format!("Connection failed: {}", result))
    }
}

/// Push the scrcpy-server.jar to the device
async fn push_server(device_serial: &str, server_jar_path: &str) -> Result<(), String> {
    let adb = find_adb();
    let output = Command::new(&adb)
        .args(["-s", device_serial, "push", server_jar_path, "/data/local/tmp/scrcpy-server.jar"])
        .output()
        .await
        .map_err(|e| format!("adb push failed: {}", e))?;

    if !output.status.success() {
        return Err(format!("adb push failed: {}", String::from_utf8_lossy(&output.stderr)));
    }

    log::info!("scrcpy-server.jar pushed to {}", device_serial);
    Ok(())
}

/// Set up ADB port forward for scrcpy
async fn setup_forward(device_serial: &str) -> Result<(), String> {
    let adb = find_adb();

    // Remove old forward if any
    let _ = Command::new(&adb)
        .args(["-s", device_serial, "forward", "--remove", &format!("tcp:{}", SCRCPY_LOCAL_PORT)])
        .output()
        .await;

    let output = Command::new(&adb)
        .args(["-s", device_serial, "forward", &format!("tcp:{}", SCRCPY_LOCAL_PORT), "localabstract:scrcpy"])
        .output()
        .await
        .map_err(|e| format!("adb forward failed: {}", e))?;

    if !output.status.success() {
        return Err(format!("adb forward failed: {}", String::from_utf8_lossy(&output.stderr)));
    }

    Ok(())
}

/// Launch scrcpy-server on the Android device
async fn launch_scrcpy_server(device_serial: &str) -> Result<tokio::process::Child, String> {
    let adb = find_adb();

    let child = Command::new(&adb)
        .args([
            "-s", device_serial,
            "shell",
            &format!(
                "CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server {} tunnel_forward=true audio=false control=true video_bit_rate=2000000 max_size=1080 max_fps=30",
                SCRCPY_SERVER_VERSION
            ),
        ])
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
        .map_err(|e| format!("Failed to launch scrcpy-server: {}", e))?;

    // Wait a moment for the server to start
    tokio::time::sleep(std::time::Duration::from_millis(1500)).await;

    log::info!("scrcpy-server launched on {}", device_serial);
    Ok(child)
}

/// Connect to the scrcpy tunnel and parse the initial device info
async fn connect_scrcpy_tunnel() -> Result<(TcpStream, String, u16, u16), String> {
    // Retry connection a few times
    let mut stream = None;
    for _ in 0..10 {
        match TcpStream::connect(format!("127.0.0.1:{}", SCRCPY_LOCAL_PORT)).await {
            Ok(s) => {
                stream = Some(s);
                break;
            }
            Err(_) => {
                tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            }
        }
    }

    let mut stream = stream.ok_or("Failed to connect to scrcpy tunnel after retries")?;

    // Read the 64-byte device name header
    let mut device_name_buf = [0u8; 64];
    stream.read_exact(&mut device_name_buf).await.map_err(|e| format!("Failed to read device name: {}", e))?;
    let device_name = String::from_utf8_lossy(&device_name_buf)
        .trim_end_matches('\0')
        .to_string();

    // Read width (2 bytes) and height (2 bytes) in big-endian
    let mut size_buf = [0u8; 4];
    stream.read_exact(&mut size_buf).await.map_err(|e| format!("Failed to read screen size: {}", e))?;
    let width = u16::from_be_bytes([size_buf[0], size_buf[1]]);
    let height = u16::from_be_bytes([size_buf[2], size_buf[3]]);

    log::info!("Scrcpy device: {} ({}x{})", device_name, width, height);

    Ok((stream, device_name, width, height))
}

/// Main bridge function — orchestrates the entire flow
pub async fn start_bridge(
    device_serial: String,
    vpc_url: String,
    jwt_secret: String,
    server_jar_path: String,
    state: SharedBridgeState,
) -> Result<(), String> {
    let (shutdown_tx, mut shutdown_rx) = mpsc::channel::<()>(1);

    // Update state
    {
        let mut s = state.write().await;
        s.active = true;
        s.device_serial = Some(device_serial.clone());
        s.started_at = Some(std::time::Instant::now());
        s.frames_sent = 0;
        s.shutdown_tx = Some(shutdown_tx);
    }

    // Step 1: Push scrcpy-server
    push_server(&device_serial, &server_jar_path).await?;

    // Step 2: Set up ADB forward
    setup_forward(&device_serial).await?;

    // Step 3: Launch scrcpy-server on device
    let mut _server_process = launch_scrcpy_server(&device_serial).await?;

    // Step 4: Connect to the scrcpy tunnel
    let (scrcpy_stream, device_name, width, height) = connect_scrcpy_tunnel().await?;
    let (scrcpy_reader, _scrcpy_writer) = tokio::io::split(scrcpy_stream);
    let mut scrcpy_reader = BufReader::with_capacity(64 * 1024, scrcpy_reader);

    // Step 5: Connect WebSocket to VPS
    let ws_url = format!("{}/ws/scrcpy/bridge", vpc_url.trim_end_matches('/').replace("https://", "wss://").replace("http://", "ws://"));
    let request = http::Request::builder()
        .uri(&ws_url)
        .header("Authorization", format!("Bearer {}", jwt_secret))
        .header("Host", "gafam-bridge")
        .header("Connection", "Upgrade")
        .header("Upgrade", "websocket")
        .header("Sec-WebSocket-Version", "13")
        .header("Sec-WebSocket-Key", tokio_tungstenite::tungstenite::handshake::client::generate_key())
        .body(())
        .map_err(|e| {
            log::error!("Failed to build WS request: {}", e);
            format!("Failed to build WS request: {}", e)
        })?;

    let native_tls_connector = TlsConnector::builder()
        .danger_accept_invalid_certs(true)
        .danger_accept_invalid_hostnames(true)
        .build()
        .map_err(|e| format!("TLS build error: {}", e))?;
    let connector = Connector::NativeTls(native_tls_connector);

    let (ws_stream, _) = connect_async_tls_with_config(
        request,
        None, // config
        false, // disable_nagle
        Some(connector)
    )
    .await
    .map_err(|e| {
        log::error!("Failed to connect WebSocket to VPS: {}", e);
        format!("Failed to connect WebSocket to VPS: {}", e)
    })?;

    let (mut ws_write, mut ws_read) = ws_stream.split();

    {
        let mut s = state.write().await;
        s.vpc_connected = true;
    }

    log::info!("Bridge connected to VPS: {}", vpc_url);

    // Step 6: Send device info
    let device_info = serde_json::json!({
        "name": device_name,
        "width": width,
        "height": height,
        "rotation": 0
    });
    let info_json = serde_json::to_vec(&device_info).unwrap();
    let mut info_msg = vec![MSG_TYPE_DEVICE_INFO];
    info_msg.extend_from_slice(&info_json);
    let _ = ws_write.send(Message::Binary(info_msg.into())).await;

    // Step 7: Bidirectional relay
    let state_clone = state.clone();

    // Task: Read H.264 from scrcpy and send to VPS
    let video_task = tokio::spawn(async move {
        let mut buf = vec![0u8; 64 * 1024]; // 64KB buffer for H.264 frames
        loop {
            match scrcpy_reader.read(&mut buf).await {
                Ok(0) => break, // EOF
                Ok(n) => {
                    let mut frame_msg = Vec::with_capacity(1 + n);
                    frame_msg.push(MSG_TYPE_VIDEO);
                    frame_msg.extend_from_slice(&buf[..n]);

                    if ws_write.send(Message::Binary(frame_msg.into())).await.is_err() {
                        break;
                    }

                    let mut s = state_clone.write().await;
                    s.frames_sent += 1;
                }
                Err(_) => break,
            }
        }
    });

    // Task: Read input events from VPS and inject via scrcpy control
    let input_task = tokio::spawn(async move {
        while let Some(Ok(msg)) = ws_read.next().await {
            match msg {
                Message::Binary(data) => {
                    if data.is_empty() {
                        continue;
                    }
                    match data[0] {
                        MSG_TYPE_INPUT => {
                            // TODO: Parse input event JSON and inject via scrcpy control socket
                            // For now, log the event
                            if let Ok(event) = serde_json::from_slice::<serde_json::Value>(&data[1..]) {
                                log::debug!("Input event: {:?}", event);
                            }
                        }
                        MSG_TYPE_SHELL => {
                            // TODO: Forward to ADB shell subprocess
                            log::debug!("Shell input received");
                        }
                        _ => {}
                    }
                }
                Message::Close(_) => break,
                _ => {}
            }
        }
    });

    // Wait for shutdown signal or task completion
    tokio::select! {
        _ = shutdown_rx.recv() => {
            log::info!("Bridge shutdown requested");
        }
        _ = video_task => {
            log::info!("Video stream ended");
        }
        _ = input_task => {
            log::info!("Input stream ended");
        }
    }

    // Cleanup
    {
        let mut s = state.write().await;
        s.active = false;
        s.vpc_connected = false;
        s.device_serial = None;
        s.shutdown_tx = None;
    }

    // Kill scrcpy-server
    let adb = find_adb();
    let _ = Command::new(&adb)
        .args(["-s", &device_serial, "shell", "pkill", "-f", "scrcpy"])
        .output()
        .await;

    // Remove forward
    let _ = Command::new(&adb)
        .args(["-s", &device_serial, "forward", "--remove", &format!("tcp:{}", SCRCPY_LOCAL_PORT)])
        .output()
        .await;

    log::info!("Bridge stopped");
    Ok(())
}

/// Stop the running bridge
pub async fn stop_bridge(state: SharedBridgeState) -> Result<(), String> {
    let shutdown_tx = {
        let s = state.read().await;
        s.shutdown_tx.clone()
    };

    if let Some(tx) = shutdown_tx {
        tx.send(()).await.map_err(|_| "Failed to send shutdown signal".to_string())?;
    }

    Ok(())
}

/// Get current bridge status
pub async fn get_status(state: SharedBridgeState) -> BridgeStatus {
    let s = state.read().await;
    BridgeStatus {
        active: s.active,
        device: s.device_serial.clone(),
        vpc_connected: s.vpc_connected,
        uptime_secs: s.started_at.map(|t| t.elapsed().as_secs()).unwrap_or(0),
        frames_sent: s.frames_sent,
    }
}
