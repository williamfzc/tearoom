//! Randomness and token verification for the LAN face. Tokens are stored
//! hashed; comparisons run in constant time. Pairing codes are short-lived
//! by policy (`lan::PAIR_TTL_SECS`, `lan::PAIR_MAX_ATTEMPTS`), so a plain
//! comparison for the six-digit code is acceptable.
use anyhow::{Context, Result};
use sha2::{Digest, Sha256};

/// Random bytes from the operating system; the LAN face has no other trust
/// anchor than this entropy.
pub(super) fn random_bytes(count: usize) -> Result<Vec<u8>> {
    read_exact_from("/dev/urandom", count).context("cannot read entropy for a LAN token")
}

fn read_exact_from(path: &str, count: usize) -> Result<Vec<u8>> {
    use std::io::Read;
    let mut bytes = vec![0u8; count];
    std::fs::File::open(path)
        .and_then(|mut file| file.read_exact(&mut bytes))
        .with_context(|| format!("cannot read entropy from {path}"))?;
    Ok(bytes)
}

/// One token: 32 bytes of entropy, hex-encoded. Lives only on the phone.
pub(super) fn mint_token() -> Result<String> {
    Ok(hex::encode(random_bytes(32)?))
}

/// A uniform six-digit pairing code, rejection-sampled from four bytes.
pub(super) fn mint_code() -> Result<String> {
    const RANGE: u32 = 1_000_000;
    let limit = u32::MAX - (u32::MAX % RANGE);
    loop {
        let bytes = random_bytes(4)?;
        let raw = u32::from_be_bytes(bytes.try_into().expect("four bytes"));
        if raw < limit {
            return Ok(format!("{:06}", raw % RANGE));
        }
    }
}

pub(super) fn token_hash(raw: &str) -> String {
    hex::encode(Sha256::digest(raw.as_bytes()))
}

/// Constant-time equality over equal-length hashes; differing lengths are a
/// mismatch without leaking where.
pub(super) fn ct_eq(left: &str, right: &str) -> bool {
    let (left, right) = (left.as_bytes(), right.as_bytes());
    if left.len() != right.len() {
        return false;
    }
    left.iter()
        .zip(right)
        .fold(0u8, |diff, (a, b)| diff | (a ^ b))
        == 0
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn codes_are_six_digits() {
        for _ in 0..64 {
            let code = mint_code().unwrap();
            assert_eq!(code.len(), 6);
            assert!(code.bytes().all(|byte| byte.is_ascii_digit()));
        }
    }

    #[test]
    fn tokens_are_unique_and_hashed_deterministically() {
        let first = mint_token().unwrap();
        let second = mint_token().unwrap();
        assert_ne!(first, second);
        assert_eq!(token_hash(&first), token_hash(&first));
        assert_ne!(token_hash(&first), token_hash(&second));
    }

    #[test]
    fn ct_eq_matches_only_equal_slices() {
        assert!(ct_eq("abc", "abc"));
        assert!(!ct_eq("abc", "abd"));
        assert!(!ct_eq("abc", "ab"));
        assert!(!ct_eq("", "a"));
    }

    #[test]
    fn random_bytes_fail_when_entropy_is_absent() {
        let error = read_exact_from("/definitely/missing/urandom", 4)
            .unwrap_err()
            .to_string();
        assert!(error.contains("entropy"), "{error}");
    }
}
