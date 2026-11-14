//! Framed viewer client for `terminal.attach`: the same stream Herdr panes
//! and `hc attach` use, as a library. The PTY lives in herdr-core; a viewer
//! receives the first paint and every live byte, and may type and resize.

use anyhow::{bail, Context, Result};
use serde_json::json;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::Shutdown;
use std::os::unix::net::UnixStream;
use std::path::Path;
use std::sync::{Arc, Mutex};
use std::thread;

use herdr_core::wire;

/// One relayed frame of a panel stream.
pub enum PanelEvent {
    /// Raw VT bytes: the first paint, then live output in order.
    Data(Vec<u8>),
    /// The stream ended; the core's reason when it sent one.
    Closed(String),
}

/// A live viewer stream over one panel. Dropping it detaches; the panel
/// itself keeps running under the core.
pub struct PanelStream {
    channel: u32,
    writer: Arc<Mutex<UnixStream>>,
}

impl PanelStream {
    /// Types bytes into the panel, as a Herdr pane would.
    pub fn write(&self, bytes: &[u8]) -> Result<()> {
        self.send(wire::DATA, bytes)
    }

    /// Resizes this viewer (cols u16 LE, rows u16 LE on the wire).
    pub fn resize(&self, cols: u16, rows: u16) -> Result<()> {
        let mut payload = cols.to_le_bytes().to_vec();
        payload.extend(rows.to_le_bytes());
        self.send(wire::RESIZE, &payload)
    }

    /// Reports viewer focus, so the core knows which viewer is active.
    pub fn focus(&self, focused: bool) -> Result<()> {
        self.send(wire::FOCUS, &[u8::from(focused)])
    }

    /// Detaches this viewer; the panel keeps running.
    pub fn detach(&self) {
        let _ = self.send(wire::CLOSE, &[]);
        let _ = self.writer.lock().unwrap().shutdown(Shutdown::Both);
    }

    fn send(&self, kind: u8, payload: &[u8]) -> Result<()> {
        let mut stream = self.writer.lock().unwrap();
        stream.write_all(&wire::encode(self.channel, kind, payload))?;
        stream.flush()?;
        Ok(())
    }
}

/// Attaches to `terminal` on the hc socket at `socket` and spawns the relay
/// thread; `relay` receives events until the stream ends.
pub fn attach(
    socket: &Path,
    terminal: &str,
    cols: u16,
    rows: u16,
    history: usize,
    relay: impl Fn(PanelEvent) + Send + 'static,
) -> Result<PanelStream> {
    let stream = UnixStream::connect(socket)
        .with_context(|| format!("cannot reach the hc runtime at {}", socket.display()))?;
    let mut writer_stream = stream.try_clone().context("split the hc socket")?;
    let mut reader = BufReader::new(stream);

    // Switch the line connection into framed mode, then speak JSON-RPC on
    // channel 0, exactly as `hc attach` does.
    writer_stream.write_all(
        b"{\"jsonrpc\":\"2.0\",\"id\":0,\"method\":\"hello\",\"params\":{\"framed\":true}}\n",
    )?;
    let mut line = String::new();
    reader.read_line(&mut line)?;
    if !line.contains("\"result\"") {
        bail!("the hc runtime refused the framed handshake");
    }

    let request = json!({
        "jsonrpc": "2.0", "id": 1, "method": "terminal.attach",
        "params": { "terminal": terminal, "history": history, "cols": cols, "rows": rows },
    });
    writer_stream.write_all(&wire::encode(0, wire::DATA, request.to_string().as_bytes()))?;

    // Read until the channel-0 reply arrives; keep any early panel frames.
    let mut early: Vec<(u32, u8, Vec<u8>)> = Vec::new();
    let channel = loop {
        let (ch, kind, payload) = read_frame(&mut reader)?;
        if ch == 0 && kind == wire::DATA {
            let reply: serde_json::Value =
                serde_json::from_slice(&payload).context("invalid attach reply")?;
            if let Some(error) = reply.get("error") {
                bail!("{}", error["message"].as_str().unwrap_or("attach failed"));
            }
            break reply["result"]["channel"]
                .as_u64()
                .context("attach reply has no channel")? as u32;
        }
        early.push((ch, kind, payload));
    };

    let writer = Arc::new(Mutex::new(writer_stream));
    let panel = PanelStream {
        channel,
        writer: writer.clone(),
    };

    // Relay panel frames to the caller; credit every frame as consumed, and
    // report the end once the program finishes or the viewer detaches.
    thread::spawn(move || {
        let mut ended = false;
        for (ch, kind, payload) in early {
            if ch == channel && kind == wire::DATA {
                relay(PanelEvent::Data(payload));
            }
        }
        loop {
            match read_frame(&mut reader) {
                Ok((ch, kind, payload)) if ch == channel && kind == wire::DATA => {
                    let credit = (payload.len() as u32).to_le_bytes();
                    relay(PanelEvent::Data(payload));
                    let _ = writer.lock().unwrap().write_all(&wire::encode(
                        channel,
                        wire::CREDIT,
                        &credit,
                    ));
                }
                Ok((ch, kind, payload)) if ch == channel && kind == wire::CLOSE => {
                    relay(PanelEvent::Closed(
                        String::from_utf8_lossy(&payload).into_owned(),
                    ));
                    ended = true;
                    break;
                }
                Ok(_) => {}
                Err(_) => break,
            }
        }
        if !ended {
            relay(PanelEvent::Closed(String::new()));
        }
    });

    Ok(panel)
}

fn read_frame(reader: &mut BufReader<UnixStream>) -> Result<(u32, u8, Vec<u8>)> {
    let mut header = [0u8; wire::HEADER];
    reader.read_exact(&mut header)?;
    let (channel, kind, len) = wire::decode_header(&header)
        .map_err(|e| anyhow::anyhow!("bad frame from the hc runtime: {e}"))?;
    let mut payload = vec![0u8; len];
    reader.read_exact(&mut payload)?;
    Ok((channel, kind, payload))
}
