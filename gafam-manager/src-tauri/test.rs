use reqwest;
#[tokio::main]
async fn main() {
    let client = reqwest::Client::builder().danger_accept_invalid_certs(true).build().unwrap();
    let res = client.get("https://google.com").send().await.unwrap();
    println!("Status: {}", res.status());
}
