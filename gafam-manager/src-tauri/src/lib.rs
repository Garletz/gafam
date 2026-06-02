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

    // We borrow the Outline Manager's OAuth Client ID for testing purposes.
    // In production, GAFAM would register its own OAuth App on DigitalOcean.
    let client_id = "7f84935771d49c2331e1cfb60c7827e20eaf128103435d82ad20b3c53253b721";
    let port = 55189; // Standard Outline Manager fallback port

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
    
    // Create droplet
    create_droplet(&token).await.map_err(|e| e.to_string())?;

    Ok("GAFAM VPC Droplet created successfully!".to_string())
}

async fn create_droplet(token: &str) -> Result<(), Box<dyn std::error::Error>> {
    let client = reqwest::Client::new();
    
    let jwt_secret: String = rand::thread_rng()
        .sample_iter(&Alphanumeric)
        .take(32)
        .map(char::from)
        .collect();

    // The magical cloud-init user_data script
    let user_data = format!(
        r#"#!/bin/bash
export JWT_SECRET="{}"
# Simulating the script fetch since TonRepo doesn't exist yet:
# curl -sSL https://raw.githubusercontent.com/TonRepo/GAFAM/main/deploy-vpc.sh | bash
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

    Ok(())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![start_do_oauth])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
