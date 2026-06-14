//! piler core client — platform-agnostic game client.
//!
//! Per the architecture, this crate owns world state, the input model, and
//! rendering into an abstract RGBA [`Framebuffer`] — all with NO browser- or
//! WASM-specific dependencies, so it stays native-testable. The `webclient`
//! is a thin host that blits the framebuffer to a `<canvas>` and tunnels the
//! WebSocket; a future native client wraps this same core.

pub mod csil;

pub mod client;
pub use client::{Client, ClientError, Event};

pub mod identicon;
pub use identicon::{identicon, Identicon};

pub mod font;

pub mod app;
pub use app::App;

/// Client semver, independent of server and API versions.
pub const VERSION: &str = env!("CARGO_PKG_VERSION");

/// An owned RGBA8 pixel buffer the core renders into. The host blits this to
/// its surface (e.g. `<canvas>` via `putImageData`). All drawing is opaque
/// (alpha forced to 255) — translucency is approximated with dark solids.
pub struct Framebuffer {
    width: u32,
    height: u32,
    pixels: Vec<u8>, // row-major RGBA8, len == width*height*4
}

impl Framebuffer {
    pub fn new(width: u32, height: u32) -> Self {
        Self {
            width,
            height,
            pixels: vec![0u8; (width as usize) * (height as usize) * 4],
        }
    }

    pub fn width(&self) -> u32 {
        self.width
    }
    pub fn height(&self) -> u32 {
        self.height
    }
    pub fn as_rgba8(&self) -> &[u8] {
        &self.pixels
    }
    pub fn as_ptr(&self) -> *const u8 {
        self.pixels.as_ptr()
    }
    pub fn byte_len(&self) -> usize {
        self.pixels.len()
    }

    /// Fill the whole buffer with one color (fast u32 memset path — we may
    /// clear up to ~4K pixels every frame).
    pub fn clear(&mut self, c: [u8; 4]) {
        let v = u32::from_le_bytes([c[0], c[1], c[2], 255]);
        let (head, mid, tail) = unsafe { self.pixels.align_to_mut::<u32>() };
        let bytes = v.to_le_bytes();
        for (i, b) in head.iter_mut().enumerate() {
            *b = bytes[i % 4];
        }
        mid.fill(v);
        for (i, b) in tail.iter_mut().enumerate() {
            *b = bytes[i % 4];
        }
    }

    /// Set one opaque pixel (bounds-checked).
    #[inline]
    pub fn set_px(&mut self, x: i32, y: i32, c: [u8; 4]) {
        if x < 0 || y < 0 || x >= self.width as i32 || y >= self.height as i32 {
            return;
        }
        let i = ((y as usize) * (self.width as usize) + (x as usize)) * 4;
        self.pixels[i] = c[0];
        self.pixels[i + 1] = c[1];
        self.pixels[i + 2] = c[2];
        self.pixels[i + 3] = 255;
    }

    /// Filled rectangle (clipped to bounds), written row by row.
    pub fn fill_rect(&mut self, x: i32, y: i32, w: i32, h: i32, c: [u8; 4]) {
        let x0 = x.max(0);
        let y0 = y.max(0);
        let x1 = (x + w).min(self.width as i32);
        let y1 = (y + h).min(self.height as i32);
        if x1 <= x0 || y1 <= y0 {
            return;
        }
        let stride = self.width as usize;
        let px = [c[0], c[1], c[2], 255];
        for yy in y0..y1 {
            let row = (yy as usize) * stride;
            for xx in x0..x1 {
                let i = (row + xx as usize) * 4;
                self.pixels[i..i + 4].copy_from_slice(&px);
            }
        }
    }

    /// Alpha-blend one pixel: `a` in 0..=1 is the source coverage. Used for
    /// basic anti-aliasing (e.g. circle edges, fading particles).
    #[inline]
    pub fn blend_px(&mut self, x: i32, y: i32, c: [u8; 3], a: f32) {
        if x < 0 || y < 0 || x >= self.width as i32 || y >= self.height as i32 {
            return;
        }
        let a = a.clamp(0.0, 1.0);
        let i = ((y as usize) * (self.width as usize) + (x as usize)) * 4;
        for k in 0..3 {
            let dst = self.pixels[i + k] as f32;
            self.pixels[i + k] = (c[k] as f32 * a + dst * (1.0 - a)).round() as u8;
        }
        self.pixels[i + 3] = 255;
    }

    /// Filled circle with a 1px anti-aliased edge (coverage-blended).
    pub fn fill_circle_aa(&mut self, cx: i32, cy: i32, r: i32, c: [u8; 3]) {
        if r <= 0 {
            return;
        }
        let rf = r as f32;
        for yy in (cy - r - 1)..=(cy + r + 1) {
            for xx in (cx - r - 1)..=(cx + r + 1) {
                let dx = (xx - cx) as f32;
                let dy = (yy - cy) as f32;
                let d = (dx * dx + dy * dy).sqrt();
                let cov = (rf + 0.5 - d).clamp(0.0, 1.0);
                if cov > 0.0 {
                    self.blend_px(xx, yy, c, cov);
                }
            }
        }
    }

    /// Rectangle outline of the given thickness.
    pub fn stroke_rect(&mut self, x: i32, y: i32, w: i32, h: i32, t: i32, c: [u8; 4]) {
        self.fill_rect(x, y, w, t, c); // top
        self.fill_rect(x, y + h - t, w, t, c); // bottom
        self.fill_rect(x, y, t, h, c); // left
        self.fill_rect(x + w - t, y, t, h, c); // right
    }

    /// Draw one 8x8 glyph scaled by `scale` (integer), top-left at (x,y).
    pub fn draw_char(&mut self, x: i32, y: i32, ch: char, scale: i32, c: [u8; 4]) {
        let g = font::glyph(ch);
        for (row, bits) in g.iter().enumerate() {
            for col in 0..8 {
                if (bits >> col) & 1 == 1 {
                    self.fill_rect(
                        x + col * scale,
                        y + row as i32 * scale,
                        scale,
                        scale,
                        c,
                    );
                }
            }
        }
    }

    /// Draw a string left-to-right; returns the advance width in pixels.
    pub fn draw_text(&mut self, x: i32, y: i32, text: &str, scale: i32, c: [u8; 4]) -> i32 {
        let mut cx = x;
        for ch in text.chars() {
            self.draw_char(cx, y, ch, scale, c);
            cx += 8 * scale;
        }
        cx - x
    }

    /// Pixel width a string will occupy at the given scale.
    pub fn text_width(text: &str, scale: i32) -> i32 {
        text.chars().count() as i32 * 8 * scale
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn framebuffer_allocates_rgba8() {
        let fb = Framebuffer::new(4, 3);
        assert_eq!(fb.as_rgba8().len(), 4 * 3 * 4);
    }

    #[test]
    fn fill_and_text_write_pixels() {
        let mut fb = Framebuffer::new(40, 16);
        fb.clear([0, 0, 0, 255]);
        fb.fill_rect(0, 0, 8, 8, [255, 0, 0, 255]);
        assert_eq!(&fb.as_rgba8()[0..4], &[255, 0, 0, 255]);
        // Drawing a glyph sets at least one pixel somewhere.
        let before = fb.as_rgba8().to_vec();
        fb.draw_text(10, 0, "A", 1, [255, 255, 255, 255]);
        assert_ne!(before, fb.as_rgba8());
    }
}
