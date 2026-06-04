use tauri::{AppHandle, Manager};
use axum::{routing::{get, post}, Router, response::{Html}, extract::Form};
use serde::Deserialize;
use rand::{distributions::Alphanumeric, Rng};
use tokio::net::TcpListener;
use std::sync::Arc;
use tokio::sync::oneshot;
use tauri_plugin_opener::OpenerExt;

#[derive(Deserialize)]
struct OauthCallback {
    params: String,
}

#[tauri::command]
async fn start_do_oauth(app: AppHandle) -> Result<String, String> {
    let state_secret: String = rand::thread_rng()
        .sample_iter(&Alphanumeric)
        .take(16)
        .map(char::from)
        .collect();

    // Use the user's provided OAuth Client ID
    let client_id = "07c3e339b16ffe83e08f31e9c5a7b223feddb81d73437ddd4bca1a454968ba94";
    let port = 55189; // Standard callback port

    let (tx, rx) = oneshot::channel::<String>();
    let tx = Arc::new(tokio::sync::Mutex::new(Some(tx)));

    let expected_state = state_secret.clone();
    
    let app_state = Router::new()
        .route("/", get(|| async {
            Html(r#"
            <html>
                <head><title>Authenticating...</title></head>
                <body>
                    <noscript>You need to enable JavaScript.</noscript>
                    <form id="form" method="POST">
                        <input id="params" type="hidden" name="params"></input>
                    </form>
                    <script>
                        var paramsStr = location.hash.substr(1);
                        var form = document.getElementById("form");
                        document.getElementById("params").setAttribute("value", paramsStr);
                        form.submit();
                    </script>
                </body>
            </html>
            "#)
        }))
        .route("/", post({
            let tx = tx.clone();
            move |Form(form): Form<OauthCallback>| {
                let tx = tx.clone();
                let expected_state = expected_state.clone();
                async move {
                    let params: std::collections::HashMap<String, String> = url::form_urlencoded::parse(form.params.as_bytes())
                        .into_owned()
                        .collect();
                    
                    if let Some(state) = params.get("state") {
                        if state == &expected_state {
                            if let Some(token) = params.get("access_token") {
                                if let Some(sender) = tx.lock().await.take() {
                                    let _ = sender.send(token.clone());
                                }
                                return Html(r#"<html><script>window.close()</script><body>Authentication successful. You can close this window.</body></html>"#);
                            }
                        }
                    }
                    Html(r#"<html><script>window.close()</script><body>Authentication failed.</body></html>"#)
                }
            }
        }));

    // Start local server
    let listener = TcpListener::bind(format!("127.0.0.1:{}", port)).await.map_err(|e| e.to_string())?;
    
    tokio::spawn(async move {
        let _ = axum::serve(listener, app_state).await;
    });

    // Open browser
    let auth_url = format!(
        "https://cloud.digitalocean.com/v1/oauth/authorize?client_id={}&response_type=token&scope=read%20write&redirect_uri=http://localhost:{}/&state={}",
        client_id, port, state_secret
    );
    
    app.opener().open_url(auth_url, None::<&str>).map_err(|e| e.to_string())?;

    // Wait for the token from the HTTP callback
    let token = rx.await.map_err(|_| "Failed to retrieve OAuth token".to_string())?;
    
    // Create droplet and get its IP
    let (ip, jwt_secret) = create_droplet(&token).await.map_err(|e| e.to_string())?;

    let response_json = serde_json::json!({
        "url": format!("http://{}:5150", ip),
        "token": jwt_secret
    });

    Ok(response_json.to_string())
}

async fn create_droplet(token: &str) -> Result<(String, String), Box<dyn std::error::Error>> {
    let client = reqwest::Client::new();
    
    let jwt_secret: String = rand::thread_rng()
        .sample_iter(&Alphanumeric)
        .take(32)
        .map(char::from)
        .collect();

    let user_data = format!(
        r#"#!/bin/bash
export JWT_SECRET="{}"
curl -sSL https://raw.githubusercontent.com/Garletz/gafam/main/deploy-vpc.sh | bash
echo "GAFAM VPC DEPLOYED" > /root/gafam_status.log
"#, jwt_secret
    );

    let payload = serde_json::json!({
        "name": format!("gafam-vpc-{:04}", rand::thread_rng().gen_range(1000..9999)),
        "region": "fra1",
        "size": "s-1vcpu-1gb",
        "image": "ubuntu-24-04-x64",
        "user_data": user_data,
        "ipv6": true,
        "tags": ["gafam-vpc"]
    });

    let res = client.post("https://api.digitalocean.com/v2/droplets")
        .bearer_auth(token)
        .json(&payload)
        .send()
        .await?;

    if !res.status().is_success() {
        let err_text = res.text().await?;
        return Err(format!("DigitalOcean API Error: {}", err_text).into());
    }

    let create_json: serde_json::Value = res.json().await?;
    let droplet_id = create_json["droplet"]["id"].as_u64().ok_or("No droplet ID returned")?;

    // Poll for the IPv4 address
    let mut ip_address = String::new();
    for _ in 0..30 { // Poll for up to ~90 seconds
        tokio::time::sleep(std::time::Duration::from_secs(3)).await;
        
        let get_res = client.get(format!("https://api.digitalocean.com/v2/droplets/{}", droplet_id))
            .bearer_auth(token)
            .send()
            .await?;
            
        if get_res.status().is_success() {
            let droplet_info: serde_json::Value = get_res.json().await?;
            let status = droplet_info["droplet"]["status"].as_str().unwrap_or("");
            
            if status == "active" {
                if let Some(v4_nets) = droplet_info["droplet"]["networks"]["v4"].as_array() {
                    for net in v4_nets {
                        if net["type"].as_str() == Some("public") {
                            if let Some(ip) = net["ip_address"].as_str() {
                                ip_address = ip.to_string();
                                break;
                            }
                        }
                    }
                }
            }
        }
        
        if !ip_address.is_empty() {
            break;
        }
    }

    if ip_address.is_empty() {
        return Err("Droplet created but timed out waiting for public IPv4 address".into());
    }

    Ok((ip_address, jwt_secret))
}

#[tauri::command]
async fn ping_vpc(url: String) -> Result<bool, String> {
    // The VPC is now using plain HTTP because Cloudflare Workers TCP sockets
    // do not support bypassing invalid certificate errors.
    let client = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(3))
        .build()
        .map_err(|e| e.to_string())?;

    match client.get(&format!("{}/api/_ping", url)).send().await {
        Ok(res) => Ok(res.status().is_success()),
        Err(_) => Ok(false)
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![start_do_oauth, ping_vpc])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
