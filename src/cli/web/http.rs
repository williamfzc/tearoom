//! Minimal HTTP/1.1 transport for the local browser face.
//! One request per connection, bounded reads, and the loopback origin rules.

use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpStream;
use std::time::Duration;

const MAX_HEAD: usize = 16 * 1024;
const MAX_LINE: usize = 8 * 1024;
const MAX_BODY: usize = 1024 * 1024;

#[derive(Debug)]
pub struct Request {
    pub method: String,
    pub path: String,
    pub query: String,
    pub headers: Vec<(String, String)>,
    pub body: Vec<u8>,
}

impl Request {
    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(key, _)| key == name)
            .map(|(_, value)| value.as_str())
    }
}

pub fn read_request(stream: &mut TcpStream) -> Result<Request, String> {
    stream
        .set_read_timeout(Some(Duration::from_secs(5)))
        .map_err(|error| error.to_string())?;
    stream
        .set_write_timeout(Some(Duration::from_secs(5)))
        .map_err(|error| error.to_string())?;
    let mut reader = BufReader::new(stream.try_clone().map_err(|error| error.to_string())?);
    let mut head = Vec::new();
    loop {
        let mut line = Vec::new();
        let read = reader
            .by_ref()
            .take(MAX_LINE as u64)
            .read_until(b'\n', &mut line)
            .map_err(|error| error.to_string())?;
        if read == 0 {
            return Err("empty request".into());
        }
        if line.last() != Some(&b'\n') {
            return Err("request line too long".into());
        }
        head.extend_from_slice(&line);
        if head.len() > MAX_HEAD {
            return Err("request headers too large".into());
        }
        if line == b"\r\n" || line == b"\n" {
            break;
        }
    }
    let text = std::str::from_utf8(&head).map_err(|_| "request headers are not UTF-8")?;
    let mut lines = text.lines();
    let request_line = lines.next().ok_or("empty request")?;
    let mut parts = request_line.split_whitespace();
    let (Some(method), Some(target), Some(version), None) =
        (parts.next(), parts.next(), parts.next(), parts.next())
    else {
        return Err("malformed request line".into());
    };
    if !matches!(version, "HTTP/1.1" | "HTTP/1.0") {
        return Err("unsupported HTTP version".into());
    }
    let target = target
        .strip_prefix('/')
        .ok_or("only origin-form request targets are accepted")?;
    let (path, query) = target
        .split_once('?')
        .map_or((target, ""), |(path, query)| (path, query));
    if path.contains('#') {
        return Err("request target must not contain a fragment".into());
    }
    let headers = parse_headers(lines)?;
    if headers.iter().any(|(name, _)| name == "transfer-encoding") {
        return Err("transfer-encoding is not supported".into());
    }
    let length = match headers.iter().find(|(name, _)| name == "content-length") {
        Some((_, value)) => {
            if value.is_empty() || !value.bytes().all(|byte| byte.is_ascii_digit()) {
                return Err("invalid content-length".into());
            }
            value
                .parse::<usize>()
                .map_err(|_| "invalid content-length")?
        }
        None => 0,
    };
    if length > MAX_BODY {
        return Err("request body too large".into());
    }
    let mut body = vec![0; length];
    reader
        .read_exact(&mut body)
        .map_err(|_| "incomplete request body")?;
    Ok(Request {
        method: method.into(),
        path: format!("/{path}"),
        query: query.into(),
        headers,
        body,
    })
}

fn parse_headers<'a>(
    lines: impl Iterator<Item = &'a str>,
) -> Result<Vec<(String, String)>, String> {
    let mut headers = Vec::new();
    for line in lines {
        if line.is_empty() {
            break;
        }
        if line.starts_with([' ', '\t']) {
            return Err("folded headers are not supported".into());
        }
        let (name, value) = line.split_once(':').ok_or("malformed header")?;
        let name = name.trim().to_ascii_lowercase();
        if name.is_empty() {
            return Err("malformed header".into());
        }
        let sensitive = matches!(
            name.as_str(),
            "host" | "origin" | "content-length" | "x-tearoom-token" | "sec-fetch-site"
        );
        if sensitive && headers.iter().any(|(current, _)| current == &name) {
            return Err("duplicate security header".into());
        }
        headers.push((name, value.trim().into()));
    }
    Ok(headers)
}

pub fn respond(
    stream: &mut TcpStream,
    status: u16,
    content_type: &str,
    body: &[u8],
) -> std::io::Result<()> {
    let reason = match status {
        200 => "OK",
        201 => "Created",
        400 => "Bad Request",
        403 => "Forbidden",
        404 => "Not Found",
        405 => "Method Not Allowed",
        413 => "Payload Too Large",
        415 => "Unsupported Media Type",
        431 => "Request Header Fields Too Large",
        500 => "Internal Server Error",
        _ => "Error",
    };
    let head = format!(
        "HTTP/1.1 {status} {reason}\r\nContent-Type: {content_type}\r\nContent-Length: {}\r\nConnection: close\r\nCache-Control: no-store\r\nX-Content-Type-Options: nosniff\r\nX-Frame-Options: DENY\r\nReferrer-Policy: no-referrer\r\nContent-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'\r\n\r\n",
        body.len()
    );
    stream.write_all(head.as_bytes())?;
    stream.write_all(body)?;
    stream.flush()
}

pub fn host_allowed(host: &str, port: u16) -> bool {
    if host.eq_ignore_ascii_case(&format!("127.0.0.1:{port}"))
        || host.eq_ignore_ascii_case(&format!("localhost:{port}"))
    {
        return true;
    }
    port == 80 && (host.eq_ignore_ascii_case("127.0.0.1") || host.eq_ignore_ascii_case("localhost"))
}

pub fn origin_allowed(origin: &str, port: u16) -> bool {
    if origin.eq_ignore_ascii_case(&format!("http://127.0.0.1:{port}"))
        || origin.eq_ignore_ascii_case(&format!("http://localhost:{port}"))
    {
        return true;
    }
    port == 80
        && (origin.eq_ignore_ascii_case("http://127.0.0.1")
            || origin.eq_ignore_ascii_case("http://localhost"))
}

/// Compare a caller-supplied token without an early exit on the first byte.
pub fn same_token(given: &str, expected: &str) -> bool {
    let given = given.as_bytes();
    let expected = expected.as_bytes();
    if given.len() != expected.len() {
        return false;
    }
    given
        .iter()
        .zip(expected)
        .fold(0u8, |diff, (a, b)| diff | (a ^ b))
        == 0
}

pub fn decode_component(value: &str) -> Result<String, String> {
    let bytes = value.as_bytes();
    let mut decoded = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        if bytes[index] == b'%' {
            let pair = bytes
                .get(index + 1..index + 3)
                .ok_or("incomplete percent escape")?;
            let pair = std::str::from_utf8(pair).map_err(|_| "invalid percent escape")?;
            decoded.push(u8::from_str_radix(pair, 16).map_err(|_| "invalid percent escape")?);
            index += 3;
        } else {
            decoded.push(bytes[index]);
            index += 1;
        }
    }
    String::from_utf8(decoded).map_err(|_| "path is not UTF-8".into())
}

pub fn query_param(query: &str, name: &str) -> Result<Option<String>, String> {
    for pair in query.split('&').filter(|pair| !pair.is_empty()) {
        let (key, value) = pair.split_once('=').map_or((pair, ""), |(k, v)| (k, v));
        if decode_component(key)? == name {
            return Ok(Some(decode_component(value)?));
        }
    }
    Ok(None)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loopback_hosts_and_origins_are_exact() {
        assert!(host_allowed("127.0.0.1:8787", 8787));
        assert!(host_allowed("LOCALHOST:8787", 8787));
        assert!(!host_allowed("evil.example:8787", 8787));
        assert!(!host_allowed("127.0.0.1:8788", 8787));
        assert!(origin_allowed("http://127.0.0.1:8787", 8787));
        assert!(!origin_allowed("https://127.0.0.1:8787", 8787));
        assert!(!origin_allowed("http://localhost.evil:8787", 8787));
    }

    #[test]
    fn token_comparison_is_exact() {
        assert!(same_token("abc", "abc"));
        assert!(!same_token("abc", "abd"));
        assert!(!same_token("abc", "ab"));
    }

    #[test]
    fn components_decode_and_reject_malformed_input() {
        assert_eq!(decode_component("a%20b").unwrap(), "a b");
        assert_eq!(decode_component("%E5%AE%A1").unwrap(), "审");
        assert!(decode_component("%2").is_err());
        assert!(decode_component("%zz").is_err());
        assert_eq!(
            query_param("after=e2&tail=5", "after").unwrap(),
            Some("e2".into())
        );
        assert_eq!(query_param("x=1", "after").unwrap(), None);
    }
}
