//! `tearoom web`: serve the local browser face on the loopback interface.
//! The command owns binding, the page token, and the accept loop.

mod app;
mod http;

use super::{open, parse};
use crate::store::Store;
use anyhow::{Context, Result};
use serde_json::json;
use std::io::{Read, Write};
use std::net::TcpListener;

pub(super) fn web_command(args: &[String]) -> Result<()> {
    let args = parse(args, &["port"], &[])?;
    if args.help(
        "Usage: tearoom web [--port <port>]\n\nServes a local browser interface on 127.0.0.1.\nThe interface can navigate rooms, read their history, and send messages\nwith explicit @role addresses. It is not reachable from other hosts.\nThe printed JSON line carries the URL; press Ctrl-C to stop.",
    ) {
        return Ok(());
    }
    args.no_positional()?;
    let port: u16 = if args.get("port").trim().is_empty() {
        0
    } else {
        args.get("port")
            .parse()
            .map_err(|_| anyhow::anyhow!("--port must be an integer between 0 and 65535"))?
    };
    serve(open()?, port)
}

fn serve(store: Store, port: u16) -> Result<()> {
    let listener = TcpListener::bind(("127.0.0.1", port))
        .with_context(|| format!("cannot bind 127.0.0.1:{port}"))?;
    let port = listener.local_addr()?.port();
    let url = format!("http://127.0.0.1:{port}/");
    let app = app::App {
        store,
        token: token()?,
        port,
    };
    println!("{}", serde_json::to_string(&json!({ "url": url }))?);
    std::io::stdout().flush()?;
    for incoming in listener.incoming() {
        let mut stream = match incoming {
            Ok(stream) => stream,
            Err(error) => {
                eprintln!("tearoom web: accept failed: {error}");
                continue;
            }
        };
        match http::read_request(&mut stream) {
            Ok(request) => {
                let reply = app.dispatch(&request);
                let _ = app::write(&mut stream, &reply);
            }
            Err(error) => {
                let body = json!({ "error": format!("bad request: {error}") }).to_string();
                let status = match error.as_str() {
                    "request body too large" => 413,
                    "request headers too large" => 431,
                    _ => 400,
                };
                let _ = http::respond(
                    &mut stream,
                    status,
                    "application/json; charset=utf-8",
                    body.as_bytes(),
                );
            }
        }
    }
    Ok(())
}

fn token() -> Result<String> {
    let mut bytes = [0u8; 32];
    std::fs::File::open("/dev/urandom")
        .context("cannot read a page token from /dev/urandom")?
        .read_exact(&mut bytes)?;
    Ok(hex::encode(bytes))
}
