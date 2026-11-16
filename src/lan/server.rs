//! Minimal HTTP/1.1 serving for the LAN face: one thread per connection,
//! strict parsing of exactly what the phone client sends, `Connection: close`
//! responses. The accept loop polls the shared state so a `pair` request can
//! enable the face or move its port without a restart.
use super::api::{self, Response};
use super::{Sender, Shared, MAX_BODY, MAX_CONSUME, MAX_HEADERS};
use std::io::{Read, Write};
use std::net::{TcpListener, TcpStream};
use std::time::Duration;

/// One parsed request: the pieces the API layer is allowed to see.
pub(super) struct Request {
    pub(super) method: String,
    pub(super) target: String,
    pub(super) authorization: Option<String>,
    pub(super) body: Vec<u8>,
}

const REASONS: &[(u16, &str)] = &[
    (200, "OK"),
    (204, "No Content"),
    (400, "Bad Request"),
    (401, "Unauthorized"),
    (403, "Forbidden"),
    (404, "Not Found"),
    (405, "Method Not Allowed"),
    (408, "Request Timeout"),
    (413, "Payload Too Large"),
    (431, "Request Header Fields Too Large"),
    (500, "Internal Server Error"),
];

fn reason(status: u16) -> &'static str {
    REASONS
        .iter()
        .find(|(code, _)| *code == status)
        .map_or("Unknown", |(_, reason)| reason)
}

pub(super) fn run(shared: Shared, sender: Sender) {
    // `bound_for` is the configured port the listener was opened for, so an
    // ephemeral bind (port 0) is not mistaken for a port change every tick.
    let mut listener: Option<(TcpListener, u16)> = None;
    loop {
        let snapshot = shared.lock().expect("lan state mutex").clone();
        let rebind = match &listener {
            Some((_, bound_for)) => !snapshot.enabled || *bound_for != snapshot.port,
            None => snapshot.enabled,
        };
        if rebind {
            listener = None;
        }
        if listener.is_none() && snapshot.enabled {
            listener = match TcpListener::bind(("0.0.0.0", snapshot.port)) {
                Ok(listener) => {
                    {
                        let mut state = shared.lock().expect("lan state mutex");
                        state.bound = listener.local_addr().ok();
                        state.bind_error.clear();
                    }
                    let bound_for = snapshot.port;
                    listener
                        .set_nonblocking(true)
                        .expect("nonblocking listener");
                    Some((listener, bound_for))
                }
                Err(error) => {
                    let mut state = shared.lock().expect("lan state mutex");
                    state.bound = None;
                    state.bind_error = format!("cannot bind 0.0.0.0:{}: {error}", snapshot.port);
                    None
                }
            };
        }
        let accepted = match &listener {
            Some((listener, _)) => listener
                .accept()
                .ok()
                .map(|(stream, _)| {
                    let shared = shared.clone();
                    let sender = sender.clone();
                    std::thread::spawn(move || handle(stream, &shared, &sender));
                })
                .is_some(),
            None => false,
        };
        if !accepted {
            std::thread::sleep(Duration::from_millis(if snapshot.enabled {
                50
            } else {
                250
            }));
        }
    }
}

fn handle(mut stream: TcpStream, shared: &Shared, sender: &Sender) {
    // On macOS an accepted socket inherits the listener's nonblocking flag;
    // this thread serves one connection, so blocking reads with a timeout
    // are the honest mode.
    let _ = stream.set_nonblocking(false);
    let _ = stream.set_read_timeout(Some(Duration::from_secs(10)));
    let _ = stream.set_write_timeout(Some(Duration::from_secs(10)));
    let response = match read_request(&mut stream) {
        Ok(request) => api::route(shared, sender, &request),
        Err(response) => response,
    };
    let _ = write_response(&mut stream, &response);
    // Half-close so the refusal reaches the client, then discard whatever
    // else it had already put on the wire — dropping an unread request body
    // would reset the connection and erase the refusal.
    let _ = stream.shutdown(std::net::Shutdown::Write);
    let deadline = std::time::Instant::now() + Duration::from_secs(2);
    let mut sink = [0u8; 8192];
    while std::time::Instant::now() < deadline {
        match stream.read(&mut sink) {
            Ok(0) | Err(_) => break,
            Ok(_) => {}
        }
    }
}

/// Read one request; any deviation from the shape the phone client sends is
/// rejected rather than interpreted.
fn read_request(stream: &mut TcpStream) -> Result<Request, Response> {
    let mut buffer: Vec<u8> = Vec::new();
    let mut chunk = [0u8; 8192];
    let head_end = loop {
        if let Some(end) = find(&buffer, b"\r\n\r\n") {
            if end > MAX_HEADERS {
                return Err(bad(431, "request headers exceed the limit"));
            }
            break end;
        }
        if buffer.len() > MAX_HEADERS + MAX_BODY {
            return Err(bad(431, "request headers exceed the limit"));
        }
        match stream.read(&mut chunk) {
            Ok(0) => return Err(bad(400, "incomplete request")),
            Ok(read) => buffer.extend_from_slice(&chunk[..read]),
            Err(error) if is_timeout(&error) => return Err(bad(408, "request timed out")),
            Err(_) => return Err(bad(400, "unreadable request")),
        }
    };
    let head = String::from_utf8_lossy(&buffer[..head_end]).into_owned();
    let mut lines = head.split("\r\n");
    let request_line = lines.next().unwrap_or_default();
    let mut parts = request_line.split(' ');
    let (method, target, version) = (
        parts.next().unwrap_or_default(),
        parts.next().unwrap_or_default(),
        parts.next().unwrap_or_default(),
    );
    if !version.starts_with("HTTP/1.") || method.is_empty() || target.is_empty() {
        return Err(bad(400, "malformed request line"));
    }
    let mut authorization = None;
    let mut content_length: Option<usize> = None;
    for line in lines {
        let Some((name, value)) = line.split_once(':') else {
            continue;
        };
        let name = name.trim().to_ascii_lowercase();
        let value = value.trim();
        if name == "authorization" {
            authorization = Some(value.to_owned());
        } else if name == "content-length" {
            content_length = Some(
                value
                    .parse()
                    .map_err(|_| bad(400, "invalid content length"))?,
            );
        } else if name == "transfer-encoding" {
            return Err(bad(400, "chunked transfer encoding is not supported"));
        }
    }
    let body_start = head_end + 4;
    let body_length = content_length.unwrap_or(0);
    if body_length > MAX_CONSUME {
        return Err(bad(413, "request body exceeds the limit"));
    }
    let wanted = body_start + body_length;
    while buffer.len() < wanted {
        match stream.read(&mut chunk) {
            Ok(0) => return Err(bad(400, "incomplete request body")),
            Ok(read) => buffer.extend_from_slice(&chunk[..read]),
            Err(error) if is_timeout(&error) => return Err(bad(408, "request timed out")),
            Err(_) => return Err(bad(400, "unreadable request body")),
        }
    }
    buffer.truncate(wanted);
    if body_length > MAX_BODY {
        // Refuse only after the declared body has been consumed, so the
        // client is never mid-write when the refusal closes the connection.
        return Err(bad(413, "request body exceeds the limit"));
    }
    Ok(Request {
        method: method.to_owned(),
        target: target.to_owned(),
        authorization,
        body: buffer[body_start..].to_vec(),
    })
}

fn write_response(stream: &mut TcpStream, response: &Response) -> std::io::Result<()> {
    let mut head = format!(
        "HTTP/1.1 {} {}\r\ncontent-type: application/json\r\n{}content-length: {}\r\nconnection: close\r\n\r\n",
        response.status,
        reason(response.status),
        api::CORS_HEADERS,
        response.body.len(),
    );
    if response.status == 204 {
        // 204 responses carry no body and no content-length per RFC 9110.
        head = format!(
            "HTTP/1.1 204 No Content\r\n{}connection: close\r\n\r\n",
            api::CORS_HEADERS
        );
    }
    stream.write_all(head.as_bytes())?;
    stream.write_all(&response.body)?;
    stream.flush()
}

fn bad(status: u16, message: &str) -> Response {
    api::error(status, message)
}

fn is_timeout(error: &std::io::Error) -> bool {
    matches!(
        error.kind(),
        std::io::ErrorKind::WouldBlock | std::io::ErrorKind::TimedOut
    )
}

fn find(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    haystack
        .windows(needle.len())
        .position(|window| window == needle)
}

/// Query-string pairs as the phone client's fetch produces them, with
/// percent escapes decoded.
pub(super) fn parse_query(query: &str) -> Vec<(String, String)> {
    query
        .split('&')
        .filter(|pair| !pair.is_empty())
        .map(|pair| match pair.split_once('=') {
            Some((name, value)) => (decode(name), decode(value)),
            None => (decode(pair), String::new()),
        })
        .collect()
}

fn decode(value: &str) -> String {
    let bytes = value.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut index = 0;
    while index < bytes.len() {
        match (bytes[index], bytes.get(index + 1), bytes.get(index + 2)) {
            (b'%', Some(first), Some(second))
                if first.is_ascii_hexdigit() && second.is_ascii_hexdigit() =>
            {
                let hex = [*first, *second];
                let hex = std::str::from_utf8(&hex).expect("ascii digits");
                out.push(u8::from_str_radix(hex, 16).expect("hex digits"));
                index += 3;
            }
            (byte, _, _) => {
                out.push(byte);
                index += 1;
            }
        }
    }
    String::from_utf8_lossy(&out).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reasons_cover_every_status_the_api_returns() {
        for status in [200, 204, 400, 401, 403, 404, 405, 408, 413, 431, 500] {
            assert!(!reason(status).is_empty());
        }
    }

    #[test]
    fn find_locates_the_header_terminator() {
        let haystack = b"GET / HTTP/1.1\r\nHost: x\r\n\r\nbody";
        assert_eq!(find(haystack, b"\r\n\r\n"), Some(23));
        assert_eq!(find(haystack, b"\r\n\r\r"), None);
    }

    #[test]
    fn query_values_decode_percent_escapes() {
        let params = parse_query("room=%E8%8C%B6%E5%AE%A4&after=e12");
        assert_eq!(params[0].1, "茶室");
        assert_eq!(params[1].1, "e12");
    }

    #[test]
    fn query_without_pairs_is_empty() {
        assert!(parse_query("").is_empty());
    }
}
