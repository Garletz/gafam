#![allow(clippy::missing_errors_doc)]
#![allow(clippy::unnecessary_struct_initialization)]
#![allow(clippy::unused_async)]
use loco_rs::prelude::*;
use serde::{Deserialize, Serialize};
use sea_orm::QueryOrder;

use crate::models::_entities::sms::{ActiveModel, Entity};

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct SmsParams {
    pub sender: Option<String>,
    pub body: Option<String>,
    pub timestamp: Option<i64>,
}

#[debug_handler]
pub async fn add(State(ctx): State<AppContext>, Json(params): Json<SmsParams>) -> Result<Response> {
    let mut item = ActiveModel {
        ..Default::default()
    };
    
    item.sender = Set(params.sender);
    item.body = Set(params.body);
    item.timestamp = Set(params.timestamp);
    
    let res = item.insert(&ctx.db).await?;
    
    format::json(res)
}

#[debug_handler]
pub async fn list(State(ctx): State<AppContext>) -> Result<Response> {
    let items = Entity::find()
        .order_by_desc(crate::models::_entities::sms::Column::Timestamp)
        .all(&ctx.db)
        .await?;
    
    format::json(items)
}

pub fn routes() -> Routes {
    Routes::new()
        .prefix("api/sms/")
        .add("/", post(add))
        .add("/", get(list))
}
