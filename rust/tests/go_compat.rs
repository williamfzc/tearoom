//! Differential format-4 test against a prebuilt Go baseline in temporary workspaces.
use std::process::Command;
use tearoom::core::AppendRequest;
use tearoom::store::Store;

#[test]
#[ignore = "requires TEAROOM_GO_BIN pointing to the Go baseline"]
fn keyed_json_is_interoperable_in_both_directions() {
    let binary = std::env::var("TEAROOM_GO_BIN").expect("set TEAROOM_GO_BIN");
    let dir = tempfile::tempdir().unwrap();
    let call = |args: &[&str]| {
        let output = Command::new(&binary)
            .current_dir(dir.path())
            .args(args)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
        output.stdout
    };
    call(&["room", "init", "--name", "root"]);
    let cases = [
        "{\"z\":\"<>&\",\"a\":1}",
        "{\"z\":\"\\u003c\\u003e\\u0026\",\"a\":1}",
        "{\"z\":\"\u{2028}\",\"a\":1}",
    ];
    let store = Store::open(dir.path()).unwrap();
    for (index, content) in cases.iter().enumerate() {
        let key = format!("go-{index}");
        call(&[
            "send",
            "--room",
            "root",
            "--as",
            "a",
            "--json",
            content,
            "--idempotency-key",
            &key,
        ]);
        let record = store
            .append(
                "root",
                "a",
                AppendRequest {
                    content,
                    idempotency_key: &key,
                },
            )
            .unwrap();
        assert_eq!(record.content.get(), *content);
        let key = format!("rust-{index}");
        let record = store
            .append(
                "root",
                "a",
                AppendRequest {
                    content,
                    idempotency_key: &key,
                },
            )
            .unwrap();
        let output = call(&[
            "send",
            "--room",
            "root",
            "--as",
            "a",
            "--json",
            content,
            "--idempotency-key",
            &key,
        ]);
        let parsed: serde_json::Value = serde_json::from_slice(&output).unwrap();
        assert_eq!(parsed["seq"].as_u64(), Some(record.seq));
    }
    assert_eq!(store.read("root", 0).unwrap().len(), cases.len() * 2);
}
