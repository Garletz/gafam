use loco_rs::prelude::*;
use sea_orm::{ConnectionTrait, Statement};
use serde::{Deserialize, Serialize};

#[derive(Debug, Deserialize, Serialize)]
pub struct TrustDeviceParams {
    pub client_signature: String,
    pub fingerprint: String,
    pub ip_address: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct TrustDeviceResponse {
    pub status: String,
    pub session_token: String,
    pub server_timestamp: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct ReserveVpcParams {
    pub phone_number: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct ReserveVpcResponse {
    pub domain: String,
    pub vpc_id: String,
    pub status: String,
    pub created_at: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct VpcStatusResponse {
    pub vpc_id: String,
    pub status: String,
    pub android_emulator: String,
    pub email_server: String,
    pub telegram_bridge: String,
    pub mock_logs: Vec<String>,
}

#[debug_handler]
async fn trust_device(
    State(_ctx): State<AppContext>,
    Json(params): Json<TrustDeviceParams>,
) -> Result<Response> {
    tracing::info!(
        signature = params.client_signature,
        fingerprint = params.fingerprint,
        "Received trust-device signature request"
    );

    format::json(TrustDeviceResponse {
        status: "Trusted Node Authorized".to_string(),
        session_token: format!("session_{}", params.client_signature.chars().take(8).collect::<String>()),
        server_timestamp: chrono::Utc::now().to_rfc3339(),
    })
}

#[debug_handler]
async fn reserve_vpc(
    State(_ctx): State<AppContext>,
    Json(params): Json<ReserveVpcParams>,
) -> Result<Response> {
    let sanitized_phone = params.phone_number.replace("+", "").replace(" ", "");
    let domain = format!("{}+gafam.com", sanitized_phone);
    let vpc_id = format!("vpc_{}", sanitized_phone);

    format::json(ReserveVpcResponse {
        domain,
        vpc_id,
        status: "VPC Provisioning Initiated".to_string(),
        created_at: chrono::Utc::now().to_rfc3339(),
    })
}

#[debug_handler]
async fn vpc_status(State(_ctx): State<AppContext>) -> Result<Response> {
    format::json(VpcStatusResponse {
        vpc_id: "vpc_33612345678".to_string(),
        status: "Active / Running".to_string(),
        android_emulator: "eSIM Bridge Connected".to_string(),
        email_server: "Mini SMTP Server Active".to_string(),
        telegram_bridge: "Sync Relay Listening".to_string(),
        mock_logs: vec![
            "[VPC INIT] Initializing hardware layer...".to_string(),
            "[eSIM] Mock eSIM registered successfully.".to_string(),
            "[SMTP] Listening for SMS verification codes...".to_string(),
            "[SYNC] Device trusted session validated successfully (zero-password).".to_string(),
        ],
    })
}

#[derive(Debug, Deserialize, Serialize)]
pub struct DelegationParams {
    pub requester_phone: String,
    pub partner_phone: String,
}

#[derive(Debug, Deserialize, Serialize)]
pub struct DelegationResponse {
    pub status: String,
    pub partner_phone: String,
    pub delegation_token: String,
    pub expires_at: String,
}

#[debug_handler]
async fn request_delegation_token(
    State(_ctx): State<AppContext>,
    Json(params): Json<DelegationParams>,
) -> Result<Response> {
    tracing::info!(
        requester = params.requester_phone,
        partner = params.partner_phone,
        "Delegated P2P verification challenge requested"
    );

    format::json(DelegationResponse {
        status: "Authorization Code Dispatched to Trusted Partner Node".to_string(),
        partner_phone: params.partner_phone,
        delegation_token: "P2P_AUTH_74B9D0E2".to_string(),
        expires_at: chrono::Utc::now()
            .checked_add_signed(chrono::Duration::minutes(5))
            .unwrap()
            .to_rfc3339(),
    })
}

#[derive(Debug, Deserialize, Serialize)]
pub struct SaveNoteParams {
    pub content: String,
}

#[debug_handler]
async fn get_note(
    State(ctx): State<AppContext>,
) -> Result<Response> {
    let db = &ctx.db;
    
    // Ensure table exists
    let _ = db.execute(Statement::from_string(
        db.get_database_backend(),
        "CREATE TABLE IF NOT EXISTS gafam_notes (id INTEGER PRIMARY KEY, content TEXT, updated_at TEXT)".to_string(),
    )).await.map_err(|e| {
        tracing::error!("Failed to create gafam_notes table: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    // Fetch the note where id = 1
    let query_res = db.query_one(Statement::from_string(
        db.get_database_backend(),
        "SELECT content FROM gafam_notes WHERE id = 1".to_string(),
    )).await.map_err(|e| {
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    let content = match query_res {
        Some(res) => {
            let val: String = res.try_get("", "content").unwrap_or_default();
            val
        }
        None => "".to_string(),
    };

    format::json(serde_json::json!({ "content": content }))
}

#[debug_handler]
async fn save_note(
    State(ctx): State<AppContext>,
    Json(params): Json<SaveNoteParams>,
) -> Result<Response> {
    let db = &ctx.db;
    
    // Ensure table exists
    let _ = db.execute(Statement::from_string(
        db.get_database_backend(),
        "CREATE TABLE IF NOT EXISTS gafam_notes (id INTEGER PRIMARY KEY, content TEXT, updated_at TEXT)".to_string(),
    )).await.map_err(|e| {
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    // UPSERT note id = 1 using standard SQLite syntax
    let stmt = Statement::from_sql_and_values(
        db.get_database_backend(),
        "INSERT INTO gafam_notes (id, content, updated_at) VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at",
        vec![
            params.content.clone().into(),
            chrono::Utc::now().to_rfc3339().into(),
        ]
    );

    let _ = db.execute(stmt).await.map_err(|e| {
        tracing::error!("UPSERT note failed: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    format::json(serde_json::json!({ "status": "saved" }))
}

#[derive(Debug, Deserialize, Serialize)]
pub struct CreateContactParams {
    pub name: String,
    pub phone_number: String,
    pub avatar_color: String,
}

#[debug_handler]
async fn get_contacts(State(ctx): State<AppContext>) -> Result<Response> {
    let db = &ctx.db;
    
    // Ensure table exists
    let _ = db.execute(Statement::from_string(
        db.get_database_backend(),
        "CREATE TABLE IF NOT EXISTS gafam_contacts (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, phone_number TEXT, avatar_color TEXT, created_at TEXT)".to_string(),
    )).await.map_err(|e| {
        tracing::error!("Failed to create gafam_contacts: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    // Query all contacts ordered by name
    let query_res = db.query_all(Statement::from_string(
        db.get_database_backend(),
        "SELECT id, name, phone_number, avatar_color, created_at FROM gafam_contacts ORDER BY name ASC".to_string(),
    )).await.map_err(|e| {
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    let mut contacts = Vec::new();
    for row in query_res {
        let id: i32 = row.try_get("", "id").unwrap_or_default();
        let name: String = row.try_get("", "name").unwrap_or_default();
        let phone_number: String = row.try_get("", "phone_number").unwrap_or_default();
        let avatar_color: String = row.try_get("", "avatar_color").unwrap_or_default();
        let created_at: String = row.try_get("", "created_at").unwrap_or_default();
        contacts.push(serde_json::json!({
            "id": id,
            "name": name,
            "phone_number": phone_number,
            "avatar_color": avatar_color,
            "created_at": created_at,
        }));
    }

    format::json(contacts)
}

#[debug_handler]
async fn create_contact(
    State(ctx): State<AppContext>,
    Json(params): Json<CreateContactParams>,
) -> Result<Response> {
    let db = &ctx.db;

    // Ensure table exists
    let _ = db.execute(Statement::from_string(
        db.get_database_backend(),
        "CREATE TABLE IF NOT EXISTS gafam_contacts (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, phone_number TEXT, avatar_color TEXT, created_at TEXT)".to_string(),
    )).await.map_err(|e| {
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    let stmt = Statement::from_sql_and_values(
        db.get_database_backend(),
        "INSERT INTO gafam_contacts (name, phone_number, avatar_color, created_at) VALUES (?, ?, ?, ?)",
        vec![
            params.name.into(),
            params.phone_number.into(),
            params.avatar_color.into(),
            chrono::Utc::now().to_rfc3339().into(),
        ]
    );

    let _ = db.execute(stmt).await.map_err(|e| {
        tracing::error!("Create contact failed: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    format::json(serde_json::json!({ "status": "contact_created" }))
}

#[derive(Debug, Deserialize, Serialize)]
pub struct PairDeviceParams {
    pub device_name: String,
    pub device_id: String,
}

#[debug_handler]
async fn pair_device(
    State(ctx): State<AppContext>,
    Json(params): Json<PairDeviceParams>,
) -> Result<Response> {
    let db = &ctx.db;

    // Ensure table exists
    let _ = db.execute(Statement::from_string(
        db.get_database_backend(),
        "CREATE TABLE IF NOT EXISTS gafam_devices (id INTEGER PRIMARY KEY AUTOINCREMENT, device_name TEXT, device_id TEXT UNIQUE, is_primary INTEGER, created_at TEXT)".to_string(),
    )).await.map_err(|e| {
        tracing::error!("Failed to create gafam_devices: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    // Check if any devices exist
    let query_res = db.query_one(Statement::from_string(
        db.get_database_backend(),
        "SELECT COUNT(*) as count FROM gafam_devices".to_string(),
    )).await.map_err(|e| {
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    let is_primary = match query_res {
        Some(res) => {
            let count: i64 = res.try_get("", "count").unwrap_or(0);
            if count == 0 { 1 } else { 0 } // First device is primary
        }
        None => 1,
    };

    let stmt = Statement::from_sql_and_values(
        db.get_database_backend(),
        "INSERT INTO gafam_devices (device_name, device_id, is_primary, created_at) VALUES (?, ?, ?, ?) ON CONFLICT(device_id) DO UPDATE SET is_primary = is_primary",
        vec![
            params.device_name.into(),
            params.device_id.into(),
            is_primary.into(),
            chrono::Utc::now().to_rfc3339().into(),
        ]
    );

    let _ = db.execute(stmt).await.map_err(|e| {
        tracing::error!("Pair device failed: {:?}", e);
        loco_rs::Error::BadRequest(e.to_string())
    })?;

    format::json(serde_json::json!({ 
        "status": "paired",
        "is_primary": is_primary == 1
    }))
}



pub fn routes() -> Routes {
    Routes::new()
        .prefix("/api/gafam")
        .add("/trust-device", post(trust_device))
        .add("/reserve-vpc", post(reserve_vpc))
        .add("/vpc-status", get(vpc_status))
        .add("/request-delegation-token", post(request_delegation_token))
        .add("/notes", get(get_note))
        .add("/notes", post(save_note))
        .add("/contacts", get(get_contacts))
        .add("/contacts", post(create_contact))
        .add("/pair-device", post(pair_device))
}
