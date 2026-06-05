use rcgen::generate_simple_self_signed;
fn main() {
    let subject_alt_names = vec!["wikipedia.org".to_string()];
    let cert = generate_simple_self_signed(subject_alt_names).unwrap();
    println!("{:?}", cert);
}
