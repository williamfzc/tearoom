//! JSONL encoding and durable file operations for the format-4 store.
use super::*;
use serde::de::DeserializeOwned;
use std::fs::OpenOptions;
use std::io::{ErrorKind, Write};

pub(super) fn sync_dir(path: &Path) -> Result<()> {
    File::open(path)?.sync_all()?;
    Ok(())
}
pub(super) fn go_escape(s: String) -> String {
    s.replace('&', "\\u0026")
        .replace('<', "\\u003c")
        .replace('>', "\\u003e")
        .replace('\u{2028}', "\\u2028")
        .replace('\u{2029}', "\\u2029")
}
pub(super) fn line(value: &impl Serialize) -> Result<Vec<u8>> {
    let mut data = go_escape(serde_json::to_string(value)?).into_bytes();
    data.push(b'\n');
    Ok(data)
}
pub(super) fn record_line(record: &Record) -> Result<Vec<u8>> {
    let field = |s: &str| -> Result<String> { Ok(go_escape(serde_json::to_string(s)?)) };
    Ok(format!(
        "{{\"room\":{},\"seq\":{},\"author\":{},\"at\":{},\"content\":{}}}\n",
        field(&record.room)?,
        record.seq,
        field(&record.author)?,
        field(&record.at)?,
        record.content.get()
    )
    .into_bytes())
}
pub(super) fn pending_line(pending: &Pending) -> Result<Vec<u8>> {
    let record = record_line(&pending.record)?;
    let ledger = line(&pending.ledger)?;
    let mut bytes = b"{\"record\":".to_vec();
    bytes.extend_from_slice(&record[..record.len() - 1]);
    bytes.extend_from_slice(b",\"ledger\":");
    bytes.extend_from_slice(&ledger[..ledger.len() - 1]);
    bytes.extend_from_slice(b"}\n");
    Ok(bytes)
}
pub(super) fn go_json_pretty(value: &impl Serialize) -> Result<Vec<u8>> {
    let mut data = go_escape(serde_json::to_string_pretty(value)?).into_bytes();
    data.push(b'\n');
    Ok(data)
}
pub(super) fn replace(path: &Path, data: &[u8]) -> Result<()> {
    let dir = path.parent().context("missing parent directory")?;
    let mut temp = tempfile::NamedTempFile::new_in(dir)?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        temp.as_file()
            .set_permissions(fs::Permissions::from_mode(0o644))?;
    }
    temp.write_all(data)?;
    temp.as_file().sync_all()?;
    temp.persist(path)?;
    sync_dir(dir)
}
pub(super) fn append_line(path: &Path, value: &impl Serialize) -> Result<()> {
    append_bytes(path, &line(value)?)
}
pub(super) fn append_bytes(path: &Path, bytes: &[u8]) -> Result<()> {
    let mut file = OpenOptions::new().create(true).append(true).open(path)?;
    file.write_all(bytes)?;
    file.sync_all()?;
    Ok(())
}
pub(super) fn read_optional(path: &Path) -> Result<Vec<u8>> {
    match fs::read(path) {
        Ok(bytes) => Ok(bytes),
        Err(e) if e.kind() == ErrorKind::NotFound => Ok(Vec::new()),
        Err(e) => Err(e.into()),
    }
}
pub(super) fn parse_lines<T: DeserializeOwned>(bytes: &[u8]) -> Result<Vec<T>> {
    bytes
        .split(|b| *b == b'\n')
        .filter(|line| !line.iter().all(u8::is_ascii_whitespace))
        .map(|line| serde_json::from_slice(line).context("corrupt JSONL entry"))
        .collect()
}
pub(super) fn read_jsonl<T: DeserializeOwned>(path: &Path) -> Result<Vec<T>> {
    let data = read_optional(path)?;
    if !data.is_empty() && !data.ends_with(b"\n") {
        bail!("incomplete JSONL tail in {}", path.display());
    }
    parse_lines(&data)
}
