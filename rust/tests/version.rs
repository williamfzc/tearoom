//! Verifies that the CLI exposes the metadata embedded at build time.

use std::process::Command;

#[test]
fn version_reports_build_metadata_and_store_format() {
    let output = Command::new(env!("CARGO_BIN_EXE_tearoom"))
        .arg("--version")
        .output()
        .unwrap();
    assert!(output.status.success());
    assert_eq!(
        String::from_utf8(output.stdout).unwrap(),
        format!(
            "tearoom version={} commit={} store-format=4\n",
            option_env!("TEAROOM_BUILD_VERSION").unwrap_or("dev"),
            option_env!("TEAROOM_BUILD_COMMIT").unwrap_or("unknown")
        )
    );
}
