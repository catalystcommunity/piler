//! piler web client — the thin WASM host.
//!
//! It wraps the platform-agnostic `coreclient::App` (which holds all state,
//! input handling, and rendering into an RGBA framebuffer) and exposes just
//! enough to JavaScript: feed inbound bytes, forward key events, advance a
//! frame, drain outbound frames to send, and read the framebuffer for blit.
//!
//! The JS host does only: WebSocket establishment + byte tunneling, input
//! forwarding, and `putImageData`. No game logic, no CBOR — all of that is
//! here/in coreclient.

use js_sys::{Array, Uint8Array};
use wasm_bindgen::prelude::*;

use piler_coreclient::App as CoreApp;

#[wasm_bindgen]
pub struct App {
    inner: CoreApp,
}

#[wasm_bindgen]
impl App {
    #[wasm_bindgen(constructor)]
    pub fn new(width: u32, height: u32, dpr: f64) -> App {
        App {
            inner: CoreApp::new(width, height, dpr),
        }
    }

    pub fn resize(&mut self, width: u32, height: u32, dpr: f64) {
        self.inner.resize(width, height, dpr);
    }

    /// Forward a key press. `key` is the DOM KeyboardEvent.key value.
    #[wasm_bindgen(js_name = keyDown)]
    pub fn key_down(&mut self, key: &str, repeat: bool) {
        self.inner.key_down(key, repeat);
    }

    #[wasm_bindgen(js_name = keyUp)]
    pub fn key_up(&mut self, key: &str) {
        self.inner.key_up(key);
    }

    /// Forward a touch/pen pointer (coordinates in CSS px; only deltas/taps
    /// are used, so the scale doesn't matter).
    #[wasm_bindgen(js_name = pointerDown)]
    pub fn pointer_down(&mut self, x: f64, y: f64) {
        self.inner.pointer_down(x as f32, y as f32);
    }

    #[wasm_bindgen(js_name = pointerMove)]
    pub fn pointer_move(&mut self, x: f64, y: f64) {
        self.inner.pointer_move(x as f32, y as f32);
    }

    #[wasm_bindgen(js_name = pointerUp)]
    pub fn pointer_up(&mut self, x: f64, y: f64) {
        self.inner.pointer_up(x as f32, y as f32);
    }

    /// True when the host should summon the soft keyboard (name entry / chat).
    #[wasm_bindgen(js_name = wantsKeyboard)]
    pub fn wants_keyboard(&self) -> bool {
        self.inner.wants_keyboard()
    }

    /// True while actively entering text (name/chat) — the host expands the
    /// hidden field to full-screen so a tap focuses it natively (reliable
    /// mobile keyboard summon).
    #[wasm_bindgen(js_name = textEntryActive)]
    pub fn text_entry_active(&self) -> bool {
        self.inner.text_entry_active()
    }

    /// The active text field's rect in CSS px as `[x, y, w, h]`, or an empty
    /// array when not entering text. The host snaps the hidden textarea here so
    /// a tap on the on-canvas box focuses it natively (mobile keyboard).
    #[wasm_bindgen(js_name = fieldRect)]
    pub fn field_rect(&self) -> Vec<f64> {
        match self.inner.field_rect() {
            Some((x, y, w, h)) => vec![x, y, w, h],
            None => Vec::new(),
        }
    }

    /// Consume a pending keyboard-focus request from the last tap (focus the
    /// hidden input within the touch gesture so mobile shows the keyboard).
    #[wasm_bindgen(js_name = takeFocusRequest)]
    pub fn take_focus_request(&mut self) -> bool {
        self.inner.take_focus_request()
    }

    /// Set the active text field (name/chat) from the hidden input's value.
    #[wasm_bindgen(js_name = setText)]
    pub fn set_text(&mut self, s: &str) {
        self.inner.set_text(s);
    }

    /// The active text field's contents (host syncs the input on focus).
    #[wasm_bindgen(js_name = currentText)]
    pub fn current_text(&self) -> String {
        self.inner.current_text()
    }

    /// Apply an inbound server frame (one WebSocket binary message).
    pub fn receive(&mut self, bytes: &[u8]) {
        self.inner.receive(bytes);
    }

    /// Advance one frame (emit movement intent + redraw the framebuffer).
    pub fn render(&mut self) {
        self.inner.render();
    }

    /// Drain queued outbound frames; the host sends each as a WS binary message.
    #[wasm_bindgen(js_name = drainOutbound)]
    pub fn drain_outbound(&mut self) -> Array {
        let arr = Array::new();
        for frame in self.inner.take_outbound() {
            arr.push(&Uint8Array::from(frame.as_slice()));
        }
        arr
    }

    /// Pointer to the RGBA framebuffer in WASM memory (for a zero-copy view).
    #[wasm_bindgen(js_name = framePtr)]
    pub fn frame_ptr(&self) -> *const u8 {
        self.inner.frame_ptr()
    }

    #[wasm_bindgen(js_name = frameLen)]
    pub fn frame_len(&self) -> usize {
        self.inner.frame_len()
    }

    pub fn width(&self) -> u32 {
        self.inner.width()
    }

    pub fn height(&self) -> u32 {
        self.inner.height()
    }
}
