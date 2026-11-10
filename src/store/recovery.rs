//! Recover interrupted keyed appends while holding the workspace lock.
use super::*;
use chrono::DateTime;
use std::fs::OpenOptions;
use std::io::{ErrorKind, Write};

impl Store {
    pub(super) fn recover(&self, room: &str) -> Result<()> {
        let pending_path = self.path(room, "append-pending.json");
        let bytes = match fs::read(&pending_path) {
            Ok(bytes) => bytes,
            Err(e) if e.kind() == ErrorKind::NotFound => return Ok(()),
            Err(e) => return Err(e.into()),
        };
        let pending: Pending = serde_json::from_slice(&bytes).context("corrupt pending append")?;
        let record = &pending.record;
        let ledger = &pending.ledger;
        let compact = compact_json(record.content.get())?;
        if record.room != room
            || record.seq == 0
            || record.seq != ledger.seq
            || ledger.key.is_empty()
            || record.author != ledger.author
            || compact != record.content.get()
            || ledger.fingerprint != fingerprint(&record.author, &compact)
        {
            bail!("invalid pending append");
        }
        let records_path = self.path(room, "records.jsonl");
        let ledger_path = self.path(room, "idempotency.jsonl");
        let records_raw = read_optional(&records_path)?;
        let ledger_raw = read_optional(&ledger_path)?;
        let (complete_records, record_tail) = split_tail(&records_raw);
        let records: Vec<Record> = parse_lines(complete_records)?;
        for (index, existing) in records.iter().enumerate() {
            if existing.room != room || existing.seq != (index + 1) as u64 {
                bail!("invalid committed record sequence");
            }
        }
        let (complete_ledger, ledger_tail) = split_tail(&ledger_raw);
        let entries: Vec<Ledger> = parse_lines(complete_ledger)?;
        for entry in &entries {
            if entry.author == ledger.author && entry.key == ledger.key && entry != ledger {
                bail!("pending append conflicts with retry ledger");
            }
        }
        let record_line = record_line(record)?;
        let ledger_line = line(ledger)?;
        let record_tail_full = validate_tail(record_tail, &record_line, |data| {
            serde_json::from_slice::<Record>(data).is_ok_and(|value| same_record(&value, record))
        })?;
        let ledger_tail_full = validate_tail(ledger_tail, &ledger_line, |data| {
            serde_json::from_slice::<Ledger>(data).is_ok_and(|value| value == *ledger)
        })?;
        let head = records.len() as u64;
        if record.seq == head {
            if !record_tail.is_empty() || head == 0 || !same_record(records.last().unwrap(), record)
            {
                bail!("pending record does not match stream head");
            }
        } else if record.seq != head + 1 {
            bail!("pending record does not follow stream head");
        }
        // Both streams must pass validation before either tail is repaired.
        repair_tail(
            &records_path,
            complete_records.len(),
            record_tail,
            record_tail_full,
        )?;
        repair_tail(
            &ledger_path,
            complete_ledger.len(),
            ledger_tail,
            ledger_tail_full,
        )?;
        if record.seq == head + 1 && !record_tail_full {
            append_bytes(&records_path, &record_line)?;
        }
        if !entries.iter().any(|e| e == ledger) && !ledger_tail_full {
            append_line(&ledger_path, ledger)?;
        }
        fs::remove_file(&pending_path)?;
        sync_dir(pending_path.parent().unwrap())?;
        Ok(())
    }
}

fn same_record(a: &Record, b: &Record) -> bool {
    let at = DateTime::parse_from_rfc3339(&a.at).ok();
    let bt = DateTime::parse_from_rfc3339(&b.at).ok();
    a.room == b.room
        && a.seq == b.seq
        && a.author == b.author
        && at.is_some()
        && at == bt
        && a.content.get() == b.content.get()
}
fn split_tail(bytes: &[u8]) -> (&[u8], &[u8]) {
    let end = bytes.iter().rposition(|b| *b == b'\n').map_or(0, |i| i + 1);
    bytes.split_at(end)
}
fn validate_tail(tail: &[u8], expected: &[u8], matches: impl Fn(&[u8]) -> bool) -> Result<bool> {
    if tail.is_empty() {
        return Ok(false);
    }
    if serde_json::from_slice::<serde_json::Value>(tail).is_ok() {
        if !matches(tail) {
            bail!("complete unterminated JSONL entry does not match pending append");
        }
        return Ok(true);
    }
    if !expected.starts_with(tail) {
        bail!("incomplete JSONL tail is not a prefix of pending append");
    }
    Ok(false)
}
fn repair_tail(path: &Path, complete_len: usize, tail: &[u8], full: bool) -> Result<()> {
    if tail.is_empty() {
        return Ok(());
    }
    let mut file = OpenOptions::new().write(true).open(path)?;
    if full {
        file = OpenOptions::new().append(true).open(path)?;
        file.write_all(b"\n")?;
    } else {
        file.set_len(complete_len as u64)?;
    }
    file.sync_all()?;
    Ok(())
}
