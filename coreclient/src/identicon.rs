//! Deterministic GitHub-style identicons.
//!
//! Given a stable seed (a player id today; a LinkKeys identity later), this
//! produces a 5×5 left-right-mirrored on/off grid plus a foreground color —
//! the same input always yields the same avatar, on any platform. Pure
//! logic with no host/wasm deps, so it lives in the core and is unit-tested
//! natively. The host draws the cells however it likes (canvas squares, a
//! framebuffer blit, etc.).

use serde::Serialize;

/// Grid dimension. 5×5 is the classic identicon size; columns 3 and 4
/// mirror columns 1 and 0, so only the left 3 columns are "free".
const SIZE: usize = 5;

/// A generated identicon: a `size`×`size` row-major grid of set cells and
/// the foreground color to draw them with.
#[derive(Debug, Clone, PartialEq, Serialize)]
pub struct Identicon {
    pub size: u32,
    /// Foreground color, RGB.
    pub color: [u8; 3],
    /// Row-major, length `size*size`. true = draw the foreground here.
    pub cells: Vec<bool>,
}

/// Generate an identicon deterministically from a seed string.
pub fn identicon(seed: &str) -> Identicon {
    let h = fnv1a(seed.as_bytes());

    let mut cells = vec![false; SIZE * SIZE];
    // Fill the left three columns (the symmetric half + center) from the low
    // 15 bits of the hash, mirroring into the right two columns.
    let mut bit = 0u32;
    for col in 0..((SIZE / 2) + 1) {
        for row in 0..SIZE {
            let on = (h >> bit) & 1 == 1;
            bit += 1;
            cells[row * SIZE + col] = on;
            cells[row * SIZE + (SIZE - 1 - col)] = on;
        }
    }

    // Derive a vivid, distinct color from high bits of the hash (kept away
    // from the cell bits so pattern and color vary independently).
    let hue = ((h >> 40) % 360) as f64;
    let color = hsl_to_rgb(hue, 0.55, 0.55);

    Identicon {
        size: SIZE as u32,
        color,
        cells,
    }
}

/// FNV-1a 64-bit — a small, stable, dependency-free hash. Deterministic
/// across platforms and toolchain versions (unlike std's DefaultHasher).
fn fnv1a(bytes: &[u8]) -> u64 {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for &b in bytes {
        h ^= b as u64;
        h = h.wrapping_mul(0x0000_0100_0000_01b3);
    }
    h
}

/// Convert HSL (h in [0,360), s/l in [0,1]) to RGB bytes.
fn hsl_to_rgb(h: f64, s: f64, l: f64) -> [u8; 3] {
    let c = (1.0 - (2.0 * l - 1.0).abs()) * s;
    let hp = h / 60.0;
    let x = c * (1.0 - (hp % 2.0 - 1.0).abs());
    let (r1, g1, b1) = match hp as u32 {
        0 => (c, x, 0.0),
        1 => (x, c, 0.0),
        2 => (0.0, c, x),
        3 => (0.0, x, c),
        4 => (x, 0.0, c),
        _ => (c, 0.0, x),
    };
    let m = l - c / 2.0;
    [
        ((r1 + m) * 255.0).round() as u8,
        ((g1 + m) * 255.0).round() as u8,
        ((b1 + m) * 255.0).round() as u8,
    ]
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn is_deterministic() {
        assert_eq!(identicon("player-1"), identicon("player-1"));
    }

    #[test]
    fn differs_by_seed() {
        assert_ne!(identicon("alice"), identicon("bob"));
    }

    #[test]
    fn grid_is_horizontally_mirrored() {
        let ic = identicon("some-uuid-here");
        let n = ic.size as usize;
        assert_eq!(ic.cells.len(), n * n);
        for row in 0..n {
            for col in 0..n {
                assert_eq!(
                    ic.cells[row * n + col],
                    ic.cells[row * n + (n - 1 - col)],
                    "cell ({row},{col}) not mirrored"
                );
            }
        }
    }
}
