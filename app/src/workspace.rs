//! Workspace selection for the desktop app. The app owns no workspace of its
//! own: it remembers the last folder the person picked and validates that it
//! still opens as a Tearoom workspace before reusing it.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::{Path, PathBuf};
use tearoom::store::Store;

#[derive(Serialize, Deserialize)]
struct Saved {
    root: PathBuf,
}

/// The remembered workspace directory, if it still opens as a workspace.
pub fn load(config_path: &Path) -> Option<PathBuf> {
    let data = fs::read(config_path).ok()?;
    let saved: Saved = serde_json::from_slice(&data).ok()?;
    validate(&saved.root).ok()
}

pub fn save(config_path: &Path, root: &Path) -> Result<()> {
    if let Some(parent) = config_path.parent() {
        fs::create_dir_all(parent)
            .with_context(|| format!("cannot create {}", parent.display()))?;
    }
    let data = serde_json::to_vec(&Saved {
        root: root.to_path_buf(),
    })?;
    fs::write(config_path, data).with_context(|| format!("cannot write {}", config_path.display()))
}

/// Validate a chosen folder by opening it; report the workspace directory
/// (the parent of the `.tearoom` marker) so a subfolder selection works.
pub fn validate(start: &Path) -> Result<PathBuf> {
    let store = Store::open(start)?;
    store
        .root()
        .parent()
        .map(Path::to_path_buf)
        .context("workspace marker has no parent directory")
}
