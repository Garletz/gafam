use std::process::Stdio;
use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::process::Command;
use tokio::sync::{mpsc, RwLock};
use tokio::net::TcpStream;
use serde::{Deserialize, Serialize};
use futures_util::{SinkExt, StreamExt};
use tokio_tungstenite::{connect_async, tungstenite::Message};

// ============================================================
// === SCRCPY BRIDGE MODULE (Manifest 14) ===
// ============================================================

/// Message type bytes matching vpc-relay/scrcpy_hub.go
const MSG_TYPE_VIDEO: u8 = 0x01;
const MSG_TYPE_INPUT: u8 = 0x02;
const MSG_TYPE_DEVICE_INFO: u8 = 0x03;
const MSG_TYPE_SHELL: u8 = 0x04;
const _MSG_TYPE_HEARTBEAT: u8 = 0x05;

#[derive(Deserialize)]
struct InputEvent {
    #[serde(rename = "type")]
    event_type: String,
    action: String,
    x: u32,
    y: u32,
}

/// Scrcpy server version to use
const SCRCPY_SERVER_VERSION: &str = "1.18";

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

/// Ship system logcat via ADB → VPC while the remote bridge is alive.
async fn ship_adb_logcat_loop(
    device_serial: String,
    vpc_host: String,
    jwt_secret: String,
    state: SharedBridgeState,
) {
    let adb = find_adb();
    let mut last_fingerprint = String::new();
    let client = match reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(8))
        .build()
    {
        Ok(c) => c,
        Err(_) => return,
    };
    let url = format!("http://{}:5150/api/auth/logs", vpc_host);

    loop {
        {
            let s = state.read().await;
            if !s.active {
                break;
            }
        }

        let output = Command::new(&adb)
            .args(["-s", &device_serial, "logcat", "-d", "-v", "threadtime", "-t", "120"])
            .output()
            .await;

        if let Ok(out) = output {
            let text = String::from_utf8_lossy(&out.stdout);
            let lines: Vec<&str> = text.lines().collect();
            if !lines.is_empty() {
                let fp = lines.last().unwrap_or(&"").to_string();
                let start = if last_fingerprint.is_empty() {
                    lines.len().saturating_sub(40)
                } else if let Some(idx) = lines.iter().position(|l| *l == last_fingerprint) {
                    idx + 1
                } else {
                    lines.len().saturating_sub(40)
                };
                last_fingerprint = fp;

                let mut entries = Vec::new();
                let now = std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map(|d| d.as_millis() as i64)
                    .unwrap_or(0);

                for (i, line) in lines[start..].iter().enumerate() {
                    if line.trim().is_empty() {
                        continue;
                    }
                    let (level, tag, message) = parse_logcat_line(line);
                    entries.push(serde_json::json!({
                        "ts": now + i as i64,
                        "source": "adb",
                        "level": level,
                        "tag": tag,
                        "message": message,
                    }));
                }

                if !entries.is_empty() {
                    let body = serde_json::json!({ "entries": entries });
                    let _ = client
                        .post(&url)
                        .header("Authorization", format!("Bearer {}", jwt_secret))
                        .json(&body)
                        .send()
                        .await;
                }
            }
        }

        tokio::time::sleep(std::time::Duration::from_secs(10)).await;
    }
}

fn parse_logcat_line(line: &str) -> (String, String, String) {
    for marker in [" V ", " D ", " I ", " W ", " E ", " F "] {
        if let Some(idx) = line.find(marker) {
            let level = marker.trim().to_string();
            let rest = line[idx + marker.len()..].trim();
            if let Some(colon) = rest.find(':') {
                let tag = rest[..colon].trim().to_string();
                let msg = rest[colon + 1..].trim().to_string();
                return (level, tag, msg);
            }
            return (level, "logcat".into(), rest.to_string());
        }
    }
    ("I".into(), "logcat".into(), line.to_string())
}

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
                "CLASSPATH=/data/local/tmp/scrcpy-server.jar app_process / com.genymobile.scrcpy.Server {} info 1080 2000000 30 -1 true - true true 0 false false i-frame-interval=2 - false",
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
async fn connect_scrcpy_tunnel() -> Result<(TcpStream, TcpStream, String, u16, u16), String> {
    // In scrcpy 1.18 (when control=true), the protocol is:
    // 1. Connect video socket
    // 2. Server sends a single dummy byte (0x00)
    // 3. Client MUST connect a second control socket
    // 4. Server then sends 64-byte device name + 4-byte video size on video socket

    // Connect Video socket
    let mut video_stream = None;
    for _ in 0..10 {
        match TcpStream::connect(format!("127.0.0.1:{}", SCRCPY_LOCAL_PORT)).await {
            Ok(s) => {
                video_stream = Some(s);
                break;
            }
            Err(_) => {
                tokio::time::sleep(std::time::Duration::from_millis(500)).await;
            }
        }
    }
    let mut video_stream = video_stream.ok_or("Failed to connect video socket to scrcpy tunnel")?;

    // Read 1 dummy byte
    let mut dummy = [0u8; 1];
    video_stream.read_exact(&mut dummy).await.map_err(|e| format!("Failed to read dummy byte: {}", e))?;

    // Connect Control socket
    let control_stream = TcpStream::connect(format!("127.0.0.1:{}", SCRCPY_LOCAL_PORT))
        .await
        .map_err(|e| format!("Failed to connect control socket: {}", e))?;

    // Now the server will send the 64-byte device name header on the video socket
    let mut device_name_buf = [0u8; 64];
    video_stream.read_exact(&mut device_name_buf).await.map_err(|e| format!("Failed to read device name: {}", e))?;
    let device_name = String::from_utf8_lossy(&device_name_buf)
        .trim_end_matches('\0')
        .to_string();

    // Read width (2 bytes) and height (2 bytes) in big-endian
    let mut size_buf = [0u8; 4];
    video_stream.read_exact(&mut size_buf).await.map_err(|e| format!("Failed to read screen size: {}", e))?;
    let width = u16::from_be_bytes([size_buf[0], size_buf[1]]);
    let height = u16::from_be_bytes([size_buf[2], size_buf[3]]);

    log::info!("Scrcpy device: {} ({}x{})", device_name, width, height);

    // We return the video_stream to read H.264 data and control_stream to send inputs
    Ok((video_stream, control_stream, device_name, width, height))
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
    eprintln!("[BRIDGE] Step 1: Pushing scrcpy-server to {}", device_serial);
    push_server(&device_serial, &server_jar_path).await?;
    eprintln!("[BRIDGE] Step 1: OK");

    // Step 2: Set up ADB forward
    eprintln!("[BRIDGE] Step 2: Setting up ADB forward");
    setup_forward(&device_serial).await?;
    eprintln!("[BRIDGE] Step 2: OK");

    // Step 3: Launch scrcpy-server on device
    eprintln!("[BRIDGE] Step 3: Launching scrcpy-server");
    let mut _server_process = launch_scrcpy_server(&device_serial).await?;
    eprintln!("[BRIDGE] Step 3: OK");

    // Step 4: Connect to the scrcpy tunnel
    eprintln!("[BRIDGE] Step 4: Connecting to scrcpy tunnel on 127.0.0.1:{}", SCRCPY_LOCAL_PORT);
    let (mut scrcpy_reader, control_stream, device_name, width, height) = match connect_scrcpy_tunnel().await {
        Ok(t) => {
            eprintln!("[BRIDGE] Step 4: OK - Device: {} ({}x{})", t.2, t.3, t.4);
            t
        }
        Err(e) => return Err(e),
    };

    // Step 5: Connect WebSocket to VPS via HTTP (port 5150) to avoid TLS issues
    // Extract the IP from the HTTPS URL and build a plain ws:// URL on port 5150
    let ip = vpc_url.trim_end_matches('/')
        .replace("https://", "")
        .replace("http://", "");
    // Remove existing port if present (e.g. :5151)
    let ip_no_port = if let Some(idx) = ip.rfind(':') {
        &ip[..idx]
    } else {
        &ip
    };
    let ws_url = format!("ws://{}:5150/ws/scrcpy/bridge?force=true", ip_no_port);
    eprintln!("[BRIDGE] Step 5: Connecting WebSocket to: {}", ws_url);
    eprintln!("[BRIDGE] Step 5: JWT token (first 8 chars): {}...", &jwt_secret[..std::cmp::min(8, jwt_secret.len())]);

    let request = http::Request::builder()
        .uri(&ws_url)
        .header("Authorization", format!("Bearer {}", jwt_secret))
        .header("Host", ip_no_port)
        .header("Connection", "Upgrade")
        .header("Upgrade", "websocket")
        .header("Sec-WebSocket-Version", "13")
        .header("Sec-WebSocket-Key", tokio_tungstenite::tungstenite::handshake::client::generate_key())
        .body(())
        .map_err(|e| {
            eprintln!("[BRIDGE] Step 5: FAILED to build request: {}", e);
            format!("Failed to build WS request: {}", e)
        })?;

    eprintln!("[BRIDGE] Step 5: Request built, attempting connect...");

    let (ws_stream, _) = connect_async(request)
        .await
        .map_err(|e| {
            eprintln!("[BRIDGE] Step 5: FAILED to connect: {}", e);
            format!("Failed to connect WebSocket to VPS: {}", e)
        })?;

    eprintln!("[BRIDGE] Step 5: WebSocket CONNECTED!");

    let (mut ws_write, mut ws_read) = ws_stream.split();

    {
        let mut s = state.write().await;
        s.vpc_connected = true;
    }

    log::info!("Bridge connected to VPS: {}", vpc_url);

    // Bonus: ship ADB logcat to VPC while bridge is active
    let log_state = state.clone();
    let log_device = device_serial.clone();
    let log_host = ip_no_port.to_string();
    let log_jwt = jwt_secret.clone();
    let _logcat_task = tokio::spawn(async move {
        ship_adb_logcat_loop(log_device, log_host, log_jwt, log_state).await;
    });

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

    // Create an mpsc channel for all websocket writes to multiplex video and shell output
    let (ws_tx, mut ws_rx) = tokio::sync::mpsc::channel::<Message>(100);
    
    let _ws_write_task = tokio::spawn(async move {
        while let Some(msg) = ws_rx.recv().await {
            if ws_write.send(msg).await.is_err() {
                break;
            }
        }
    });

    // Step 7: Bidirectional relay
    let state_clone = state.clone();
    let ws_tx_video = ws_tx.clone();

    // Task: Read H.264 from scrcpy and send to VPS
    let video_task = tokio::spawn(async move {
        let mut pts_buf = [0u8; 8];
        let mut size_buf = [0u8; 4];
        
        loop {
            // Read PTS (8 bytes)
            if scrcpy_reader.read_exact(&mut pts_buf).await.is_err() { break; }
            
            // Read Size (4 bytes)
            if scrcpy_reader.read_exact(&mut size_buf).await.is_err() { break; }
            
            let packet_size = u32::from_be_bytes(size_buf) as usize;
            
            if packet_size > 0 {
                // Read exact NAL unit packet data
                let mut packet_data = vec![0u8; packet_size];
                if scrcpy_reader.read_exact(&mut packet_data).await.is_err() { break; }
                
                let mut frame_msg = Vec::with_capacity(1 + packet_size);
                frame_msg.push(MSG_TYPE_VIDEO);
                frame_msg.extend_from_slice(&packet_data);

                if ws_tx_video.send(Message::Binary(frame_msg.into())).await.is_err() {
                    break;
                }

                let mut s = state_clone.write().await;
                s.frames_sent += 1;
            }
        }
    });

    // Task: Read input events from VPS and inject via scrcpy control
    let ws_tx_input = ws_tx.clone();
    let device_serial_clone = device_serial.to_string();
    
    // We need width and height to correctly send the touch event
    let screen_width = width;
    let screen_height = height;

    let mut control_writer = control_stream; // We use control_stream for writing inputs

    let input_task = tokio::spawn(async move {
        while let Some(Ok(msg)) = ws_read.next().await {
            match msg {
                Message::Binary(data) => {
                    if data.is_empty() {
                        continue;
                    }
                    match data[0] {
                        MSG_TYPE_INPUT => {
                            if let Ok(event) = serde_json::from_slice::<InputEvent>(&data[1..]) {
                                if event.event_type == "touch" {
                                    let action = match event.action.as_str() {
                                        "down" => 0u8,
                                        "up" => 1u8,
                                        "move" => 2u8,
                                        _ => continue,
                                    };
                                    
                                    // Scrcpy 1.18 INJECT_TOUCH_EVENT format (28 bytes)
                                    let mut packet = [0u8; 28];
                                    packet[0] = 2; // TYPE_INJECT_TOUCH_EVENT
                                    packet[1] = action;
                                    packet[2..10].copy_from_slice(&1u64.to_be_bytes()); // pointerId = 1
                                    packet[10..14].copy_from_slice(&event.x.to_be_bytes());
                                    packet[14..18].copy_from_slice(&event.y.to_be_bytes());
                                    packet[18..20].copy_from_slice(&screen_width.to_be_bytes());
                                    packet[20..22].copy_from_slice(&screen_height.to_be_bytes());
                                    packet[22..24].copy_from_slice(&0xffffu16.to_be_bytes()); // pressure = 1.0 (max)
                                    packet[24..28].copy_from_slice(&1u32.to_be_bytes()); // buttons = 1 (primary/left click)
                                    
                                    let _ = control_writer.write_all(&packet).await;
                                }
                            }
                        }
                        MSG_TYPE_SHELL => {
                            // Forward to ADB shell subprocess
                            let cmd_str = String::from_utf8_lossy(&data[1..]).to_string();
                            let device_serial = device_serial_clone.clone();
                            let ws_tx = ws_tx_input.clone();
                            
                            tokio::spawn(async move {
                                let adb = find_adb();
                                let output = Command::new(&adb)
                                    .args(["-s", &device_serial, "shell", &cmd_str])
                                    .output()
                                    .await;
                                    
                                if let Ok(out) = output {
                                    let mut resp_msg = vec![MSG_TYPE_SHELL];
                                    resp_msg.extend_from_slice(&out.stdout);
                                    resp_msg.extend_from_slice(&out.stderr);
                                    let _ = ws_tx.send(Message::Binary(resp_msg.into())).await;
                                }
                            });
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
    let exit_result = tokio::select! {
        _ = shutdown_rx.recv() => {
            log::info!("Bridge shutdown requested");
            Ok(())
        }
        _ = video_task => {
            log::info!("Video stream ended unexpectedly");
            Err("Video stream ended unexpectedly".to_string())
        }
        _ = input_task => {
            log::info!("Input stream ended unexpectedly");
            Err("Input stream ended unexpectedly".to_string())
        }
    };

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
    exit_result
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
