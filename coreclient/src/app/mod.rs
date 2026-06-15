//! The client application: UI state machine, input handling, and per-frame
//! simulation step — entirely in the framebuffer (no DOM). The host forwards
//! key events, tunnels WebSocket bytes, and blits the framebuffer. The
//! framebuffer drawing itself lives in the `render` submodule.
//!
//! Rendering is device-resolution and camera-based: the field has a fixed
//! logical size (1 tile = 40 logical px) drawn at `scale = dpr` device pixels
//! per logical pixel, so it keeps a consistent ratio to the monitor's pixel
//! density. The view is a camera centered on the player and clamped to the
//! field — resizing the window shows more/less of the field (cut off), never
//! squished.

mod render;

use std::collections::HashMap;

use crate::client::{Client, ClientError, Event};
use crate::identicon::{identicon, Identicon};
use crate::Framebuffer;

const STEP: i64 = 300; // move intent (sub-units) per frame while held
const SUBF: f64 = 1000.0; // matches server SUB
const PX_PER_TILE: f64 = 40.0; // logical px per tile
const LPP: f64 = PX_PER_TILE / SUBF; // logical px per sub-unit
const AVATAR_LOGICAL: f64 = 30.0; // avatar size in logical px
const COOLDOWN_FRAMES: i32 = 150; // 5s at 30fps
const FIRE_DUR: f32 = 28.0; // firework lifetime in frames
const FIRE_PARTICLES: i32 = 22;

const TOUCH_DEADZONE: f32 = 24.0; // px of drag before the stick engages
const DOUBLE_TAP_FRAMES: u64 = 14; // ~470ms window for a double-tap (relaxed)

const BG: [u8; 4] = [16, 16, 26, 255];
const PANEL: [u8; 4] = [22, 22, 34, 255];
const GRID: [u8; 4] = [28, 28, 48, 255];
const BORDER: [u8; 4] = [58, 58, 106, 255];
const TEXT: [u8; 4] = [215, 215, 224, 255];
const MUTED: [u8; 4] = [120, 120, 136, 255];
const RED: [u8; 4] = [240, 120, 140, 255];
const ME: [u8; 4] = [108, 204, 255, 255];

const FIRE_COLORS: [[u8; 3]; 5] = [
    [255, 220, 120],
    [255, 140, 80],
    [240, 100, 130],
    [120, 200, 255],
    [255, 255, 255],
];

#[derive(Clone, Copy, PartialEq)]
enum Screen {
    Name,
    Play,
}

#[derive(Default)]
struct Held {
    up: bool,
    down: bool,
    left: bool,
    right: bool,
}

#[derive(Clone, Copy)]
struct Firework {
    start: u64,
    x: f64, // world logical
    y: f64,
}

pub struct App {
    fb: Framebuffer,
    dpr: f64,
    client: Client,
    screen: Screen,

    name: String,
    name_taken: bool,

    chat_open: bool,
    chat: String,
    held: Held,

    // touch gesture state
    touch_start: Option<(f32, f32)>,
    touch_dragging: bool,
    last_tap_frame: Option<u64>,
    pending_tap: Option<u64>,
    // set when a tap should summon the soft keyboard; the host consumes it
    // inside the touch gesture (the only time mobile lets us show the keyboard).
    request_focus: bool,

    cooldown: i32,
    fireworks: Vec<Firework>,

    frame: u64,
    outbound: Vec<Vec<u8>>,
    avatars: HashMap<String, Identicon>,
}

impl App {
    pub fn new(width: u32, height: u32, dpr: f64) -> Self {
        App {
            fb: Framebuffer::new(width.max(1), height.max(1)),
            dpr: if dpr > 0.0 { dpr } else { 1.0 },
            client: Client::new(),
            screen: Screen::Name,
            name: String::new(),
            name_taken: false,
            chat_open: false,
            chat: String::new(),
            held: Held::default(),
            touch_start: None,
            touch_dragging: false,
            last_tap_frame: None,
            pending_tap: None,
            request_focus: false,
            cooldown: 0,
            fireworks: Vec::new(),
            frame: 0,
            outbound: Vec::new(),
            avatars: HashMap::new(),
        }
    }

    pub fn resize(&mut self, width: u32, height: u32, dpr: f64) {
        self.fb = Framebuffer::new(width.max(1), height.max(1));
        self.dpr = if dpr > 0.0 { dpr } else { 1.0 };
    }

    // --- host interface ---

    pub fn frame_ptr(&self) -> *const u8 {
        self.fb.as_ptr()
    }
    pub fn frame_len(&self) -> usize {
        self.fb.byte_len()
    }
    pub fn width(&self) -> u32 {
        self.fb.width()
    }
    pub fn height(&self) -> u32 {
        self.fb.height()
    }
    pub fn take_outbound(&mut self) -> Vec<Vec<u8>> {
        std::mem::take(&mut self.outbound)
    }

    pub fn receive(&mut self, bytes: &[u8]) {
        match self.client.apply_frame(bytes) {
            Ok(Event::Welcome) => {
                self.screen = Screen::Play;
                // Drop any name-screen tap state so it can't resolve into play.
                self.pending_tap = None;
                self.last_tap_frame = None;
            }
            Ok(Event::NameAvailability { name, available }) => {
                if name == self.name {
                    self.name_taken = !available;
                }
            }
            Ok(Event::Firework { player_id }) => {
                if let Some((lx, ly)) = self.player_logical(&player_id) {
                    self.spawn_firework(lx, ly);
                }
            }
            Ok(_) => {}
            Err(ClientError::Server { .. }) => {
                if self.screen == Screen::Name {
                    self.name_taken = true;
                }
            }
            Err(_) => {}
        }
    }

    pub fn key_down(&mut self, key: &str, _repeat: bool) {
        match self.screen {
            Screen::Name => self.name_key(key),
            Screen::Play => self.play_key(key),
        }
    }

    pub fn key_up(&mut self, key: &str) {
        match key {
            "w" | "W" | "ArrowUp" => self.held.up = false,
            "s" | "S" | "ArrowDown" => self.held.down = false,
            "a" | "A" | "ArrowLeft" => self.held.left = false,
            "d" | "D" | "ArrowRight" => self.held.right = false,
            _ => {}
        }
    }

    /// True when the host should summon/keep the soft keyboard: name entry,
    /// open chat, or a pending single-tap that's about to open chat (so the
    /// gesture-focus isn't blurred during the double-tap window).
    pub fn wants_keyboard(&self) -> bool {
        self.screen == Screen::Name || self.chat_open || self.pending_tap.is_some()
    }

    /// True only while the user is *actively* entering text (name screen or an
    /// open chat) — NOT during the pending double-tap window. The host uses
    /// this to expand the hidden text field to full-screen so a tap focuses it
    /// natively (the reliable way to summon a mobile keyboard); during play it
    /// stays out of the way so canvas gestures (drag stick, double-tap) work.
    pub fn text_entry_active(&self) -> bool {
        self.screen == Screen::Name || self.chat_open
    }

    // --- touch / pointer gestures ---
    // The host forwards pointer (touch/pen) events; we interpret them: a drag
    // past the deadzone is an 8-way movement stick, a double-tap is a
    // firework, and a single tap opens chat.

    pub fn pointer_down(&mut self, x: f32, y: f32) {
        self.touch_start = Some((x, y));
        self.touch_dragging = false;
    }

    pub fn pointer_move(&mut self, x: f32, y: f32) {
        if let Some((sx, sy)) = self.touch_start {
            let (dx, dy) = (x - sx, y - sy);
            if dx * dx + dy * dy > TOUCH_DEADZONE * TOUCH_DEADZONE {
                self.touch_dragging = true;
                self.set_drag_dir(dx, dy);
            } else if self.touch_dragging {
                self.touch_dragging = false;
                self.held = Held::default();
            }
        }
    }

    pub fn pointer_up(&mut self, _x: f32, _y: f32) {
        let was_drag = self.touch_dragging;
        self.touch_start = None;
        self.touch_dragging = false;
        if was_drag {
            self.held = Held::default(); // release the stick
            return;
        }

        // On the name screen a tap just summons the keyboard (the play-mode
        // tap gestures don't apply, and a pending tap must not leak into play
        // and open chat after joining).
        if self.screen == Screen::Name {
            self.request_focus = true;
            return;
        }

        // In play: two taps within the window = double-tap (firework);
        // otherwise a single tap resolved after the window (in render) so it
        // doesn't pre-empt a double-tap.
        if let Some(prev) = self.last_tap_frame {
            if self.frame.saturating_sub(prev) <= DOUBLE_TAP_FRAMES {
                self.last_tap_frame = None;
                self.pending_tap = None;
                self.try_firework();
                return;
            }
        }
        self.last_tap_frame = Some(self.frame);
        self.pending_tap = Some(self.frame + DOUBLE_TAP_FRAMES + 1);
        // A tap that's about to open chat should summon the keyboard.
        self.request_focus = !self.chat_open;
    }

    /// Consume a pending keyboard-focus request (host calls this right after a
    /// pointer-up to focus the hidden input within the touch gesture).
    pub fn take_focus_request(&mut self) -> bool {
        let r = self.request_focus;
        self.request_focus = false;
        r
    }

    fn set_drag_dir(&mut self, dx: f32, dy: f32) {
        if self.screen != Screen::Play || self.chat_open {
            return;
        }
        let oct = ((dy.atan2(dx) / std::f32::consts::FRAC_PI_4).round() as i32).rem_euclid(8);
        let mut h = Held::default();
        match oct {
            0 => h.right = true,
            1 => {
                h.right = true;
                h.down = true;
            }
            2 => h.down = true,
            3 => {
                h.left = true;
                h.down = true;
            }
            4 => h.left = true,
            5 => {
                h.left = true;
                h.up = true;
            }
            6 => h.up = true,
            _ => {
                h.right = true;
                h.up = true;
            }
        }
        self.held = h;
    }

    fn single_tap(&mut self) {
        if self.screen == Screen::Play && !self.chat_open {
            self.chat_open = true;
        }
    }

    /// Advance one frame: step the simulation, then draw the current screen.
    /// The single host entry point per frame.
    pub fn render(&mut self) {
        self.step();
        match self.screen {
            Screen::Name => self.draw_name(),
            Screen::Play => self.draw_play(),
        }
    }

    /// Per-frame simulation (no drawing): advance the frame clock, tick the
    /// firework cooldown, resolve a pending single-tap (which may open chat),
    /// and emit held-movement intent. Split from the drawing in `render` so the
    /// two concerns read independently.
    fn step(&mut self) {
        self.frame += 1;
        if self.cooldown > 0 {
            self.cooldown -= 1;
        }
        if let Some(deadline) = self.pending_tap {
            if self.frame >= deadline {
                self.pending_tap = None;
                self.last_tap_frame = None;
                self.single_tap();
            }
        }
        if self.screen == Screen::Play && !self.chat_open {
            self.emit_movement();
        }
    }

    // --- input ---

    /// Replace the active text field (name or chat) with the host's input
    /// value. Editing flows through the hidden DOM input's `input` event
    /// rather than keydown, which is the only reliable text source on mobile
    /// soft keyboards.
    pub fn set_text(&mut self, s: &str) {
        match self.screen {
            Screen::Name => {
                let t: String = s.chars().take(32).collect();
                if t != self.name {
                    self.name = t;
                    self.name_taken = false;
                    if self.name.chars().count() >= 3 {
                        let f = self.client.build_check_name(self.name.clone());
                        self.outbound.push(f);
                    }
                }
            }
            Screen::Play => {
                if self.chat_open {
                    self.chat = s.chars().take(500).collect();
                }
            }
        }
    }

    /// The active text field's current contents (host syncs the input to this
    /// when focusing).
    pub fn current_text(&self) -> String {
        match self.screen {
            Screen::Name => self.name.clone(),
            Screen::Play if self.chat_open => self.chat.clone(),
            Screen::Play => String::new(),
        }
    }

    fn name_key(&mut self, key: &str) {
        if key == "Enter" && self.name.chars().count() >= 3 && !self.name_taken {
            let f = self.client.build_join(self.name.clone(), None);
            self.outbound.push(f);
        }
    }

    fn play_key(&mut self, key: &str) {
        if self.chat_open {
            // Text comes via set_text; here we only handle submit/cancel.
            match key {
                "Enter" => {
                    let msg = self.chat.trim().to_string();
                    if !msg.is_empty() {
                        let f = self.client.build_say(msg);
                        self.outbound.push(f);
                    }
                    self.chat.clear();
                    self.chat_open = false;
                }
                "Escape" => {
                    self.chat.clear();
                    self.chat_open = false;
                }
                _ => {}
            }
            return;
        }
        match key {
            "Enter" => self.chat_open = true,
            " " => self.try_firework(),
            "w" | "W" | "ArrowUp" => self.held.up = true,
            "s" | "S" | "ArrowDown" => self.held.down = true,
            "a" | "A" | "ArrowLeft" => self.held.left = true,
            "d" | "D" | "ArrowRight" => self.held.right = true,
            _ => {}
        }
    }

    fn try_firework(&mut self) {
        if self.cooldown > 0 {
            return;
        }
        if let Some((lx, ly)) = self.me_logical() {
            self.spawn_firework(lx, ly);
            self.cooldown = COOLDOWN_FRAMES;
            // Tell the server so it broadcasts to everyone else in the room.
            self.outbound.push(self.client.build_firework());
        }
    }

    /// Spawn a firework above a logical (x, y) — used for our own (instant) and
    /// for remote players' fireworks received from the server.
    fn spawn_firework(&mut self, lx: f64, ly: f64) {
        // Bound the live count so a flurry can't grow unbounded.
        if self.fireworks.len() >= 32 {
            self.fireworks.remove(0);
        }
        self.fireworks.push(Firework {
            start: self.frame,
            x: lx,
            y: ly - AVATAR_LOGICAL, // above the player
        });
    }

    /// Logical (x, y) of a player in the current room, by id.
    fn player_logical(&self, player_id: &str) -> Option<(f64, f64)> {
        let room = self.client.room()?;
        let p = room.players.iter().find(|p| p.player_id == player_id)?;
        Some((
            (p.pos.tile_x as f64 * SUBF + p.pos.sub_x as f64) * LPP,
            (p.pos.tile_y as f64 * SUBF + p.pos.sub_y as f64) * LPP,
        ))
    }

    fn emit_movement(&mut self) {
        let mut dx = 0;
        let mut dy = 0;
        if self.held.left {
            dx -= STEP;
        }
        if self.held.right {
            dx += STEP;
        }
        if self.held.up {
            dy -= STEP;
        }
        if self.held.down {
            dy += STEP;
        }
        if dx != 0 || dy != 0 {
            let f = self.client.build_move(dx, dy);
            self.outbound.push(f);
        }
    }

    // --- helpers ---

    fn ui_scale(&self) -> i32 {
        ((2.0 * self.dpr).round() as i32).max(1)
    }

    /// Device-pixel rect (x, y, w, h) of the name-entry box. Single source of
    /// truth for both `draw_name` and `field_rect`.
    fn name_box_rect(&self) -> (i32, i32, i32, i32) {
        let w = self.fb.width() as i32;
        let h = self.fb.height() as i32;
        let s = self.ui_scale();
        let box_w = (w / 2).clamp(160 * s, 600 * s.max(1) / 2 + 200);
        let box_h = 14 * s;
        (((w - box_w) / 2), (h / 2 - box_h / 2), box_w, box_h)
    }

    /// Device-pixel rect (x, y, w, h) of the chat input box. Single source of
    /// truth for both `draw_chat_input` and `field_rect`.
    fn chat_box_rect(&self) -> (i32, i32, i32, i32) {
        let w = self.fb.width() as i32;
        let h = self.fb.height() as i32;
        let box_h = 12 * self.ui_scale();
        (6, h - box_h - 6, w - 12, box_h)
    }

    /// The active text field's rect in **CSS pixels** (x, y, w, h), or None when
    /// not entering text. The host snaps the hidden `<textarea>` exactly over
    /// this box so a tap focuses it natively (raising the mobile keyboard)
    /// without covering the rest of the canvas. CSS px = device px / dpr, and
    /// the canvas sits at the page origin.
    pub fn field_rect(&self) -> Option<(f64, f64, f64, f64)> {
        let (x, y, w, h) = match self.screen {
            Screen::Name => self.name_box_rect(),
            Screen::Play if self.chat_open => self.chat_box_rect(),
            Screen::Play => return None,
        };
        let d = self.dpr;
        Some((x as f64 / d, y as f64 / d, w as f64 / d, h as f64 / d))
    }

    fn cursor_on(&self) -> bool {
        (self.frame / 15) % 2 == 0
    }

    fn me_logical(&self) -> Option<(f64, f64)> {
        let me = self.client.me()?;
        Some((
            (me.pos.tile_x as f64 * SUBF + me.pos.sub_x as f64) * LPP,
            (me.pos.tile_y as f64 * SUBF + me.pos.sub_y as f64) * LPP,
        ))
    }

    fn avatar(&mut self, seed: &str) -> Identicon {
        if let Some(a) = self.avatars.get(seed) {
            return a.clone();
        }
        let a = identicon(seed);
        self.avatars.insert(seed.to_string(), a.clone());
        a
    }
}

/// Camera top-left along one axis: center on `target`, clamp to the field, or
/// center the field if it's smaller than the viewport.
fn camera(target: f64, view: f64, field: f64) -> f64 {
    if field <= view {
        (field - view) / 2.0
    } else {
        (target - view / 2.0).clamp(0.0, field - view)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn name_entry_checks_then_joins() {
        let mut app = App::new(400, 300, 1.0);
        app.set_text("abc"); // text flows through the host input
        assert!(!app.take_outbound().is_empty(), "expected a check-name frame at 3 chars");
        app.key_down("Enter", false);
        assert!(!app.take_outbound().is_empty(), "expected a join frame");
    }

    #[test]
    fn taken_name_blocks_join() {
        let mut app = App::new(400, 300, 1.0);
        app.set_text("ada");
        let _ = app.take_outbound();
        app.name_taken = true;
        app.key_down("Enter", false);
        assert!(app.take_outbound().is_empty(), "join must be blocked when taken");
    }

    #[test]
    fn set_text_edits_active_field() {
        let mut app = App::new(400, 300, 1.0);
        app.set_text("Ada");
        assert_eq!(app.current_text(), "Ada");
        app.screen = Screen::Play;
        app.set_text("ignored"); // chat closed → ignored
        assert_eq!(app.current_text(), "");
        app.chat_open = true;
        app.set_text("hi there");
        assert_eq!(app.current_text(), "hi there");
    }

    #[test]
    fn camera_centers_and_clamps() {
        // field smaller than view → field centered (negative cam to center).
        assert_eq!(camera(50.0, 200.0, 100.0), -50.0);
        // player near origin → clamped to 0.
        assert_eq!(camera(10.0, 100.0, 1000.0), 0.0);
        // player mid-field → centered.
        assert_eq!(camera(500.0, 100.0, 1000.0), 450.0);
        // player near far edge → clamped to field-view.
        assert_eq!(camera(990.0, 100.0, 1000.0), 900.0);
    }

    #[test]
    fn renders_without_panicking() {
        let mut app = App::new(320, 200, 2.0);
        app.render();
        assert_eq!(app.frame_len(), 320 * 200 * 4);
    }

    #[test]
    fn play_renders_at_high_dpr_without_panicking() {
        // High-dpr phones make ui_scale `s` large (dpr 3 → s = 6). A clamp in
        // the player-list panel whose scaled min (90*s = 540) exceeded a fixed
        // cap (520) used to panic, poisoning the wasm and freezing the client
        // right after join. Render the play screen at dpr 3 to guard it.
        use crate::csil::{JoinResponse, Player, Position, RoomState, ServerMessage};
        let mk = |id: &str, name: &str| Player {
            player_id: id.into(),
            name: name.into(),
            room_id: "r".into(),
            pos: Position { tile_x: 5, tile_y: 5, sub_x: 0, sub_y: 0, layer: 0 },
        };
        let room = RoomState {
            room_id: "r".into(),
            players: vec![mk("me", "TodPhone"), mk("b", "Bot")],
            recent_chat: vec![],
            field_w: 48000,
            field_h: 27000,
        };
        let mut body = Vec::new();
        ciborium::into_writer(&JoinResponse { player: mk("me", "TodPhone"), room }, &mut body).unwrap();
        let mut frame = Vec::new();
        ciborium::into_writer(&ServerMessage { event: "welcome".into(), body }, &mut frame).unwrap();

        let mut app = App::new(1080, 2160, 3.0); // dpr 3 → s = 6
        app.receive(&frame);
        assert!(app.screen == Screen::Play, "welcome should switch to Play");
        app.render(); // must not panic
        assert_eq!(app.frame_len(), 1080 * 2160 * 4);
    }

    #[test]
    fn drag_sets_eight_way_direction() {
        let mut app = App::new(400, 300, 1.0);
        app.screen = Screen::Play;
        app.pointer_down(100.0, 100.0);
        app.pointer_move(100.0, 200.0); // straight down
        assert!(app.held.down && !app.held.up && !app.held.left && !app.held.right);
        app.pointer_move(160.0, 40.0); // up-right
        assert!(app.held.up && app.held.right);
        app.pointer_up(160.0, 40.0); // release stops
        assert!(!app.held.up && !app.held.down && !app.held.left && !app.held.right);
    }

    #[test]
    fn single_tap_opens_chat_after_window() {
        let mut app = App::new(400, 300, 1.0);
        app.screen = Screen::Play;
        app.pointer_down(10.0, 10.0);
        app.pointer_up(10.0, 10.0);
        assert!(!app.chat_open, "chat should not open before the double-tap window");
        for _ in 0..(DOUBLE_TAP_FRAMES + 3) {
            app.render();
        }
        assert!(app.chat_open, "single tap should open chat after the window");
    }

    #[test]
    fn double_tap_does_not_open_chat() {
        let mut app = App::new(400, 300, 1.0);
        app.screen = Screen::Play;
        app.pointer_down(10.0, 10.0);
        app.pointer_up(10.0, 10.0); // first tap
        app.pointer_down(11.0, 11.0);
        app.pointer_up(11.0, 11.0); // second tap, same frame → double-tap
        for _ in 0..(DOUBLE_TAP_FRAMES + 3) {
            app.render();
        }
        assert!(!app.chat_open, "double-tap must not open chat");
    }

    #[test]
    fn tap_on_name_requests_keyboard_focus() {
        let mut app = App::new(400, 300, 1.0); // name screen
        app.pointer_down(10.0, 10.0);
        app.pointer_up(10.0, 10.0);
        assert!(app.take_focus_request(), "a name-screen tap should request the keyboard");
        assert!(!app.take_focus_request(), "request is consumed once");
    }

    #[test]
    fn drag_does_not_request_keyboard_focus() {
        let mut app = App::new(400, 300, 1.0);
        app.screen = Screen::Play;
        app.pointer_down(10.0, 10.0);
        app.pointer_move(200.0, 10.0); // drag, not a tap
        app.pointer_up(200.0, 10.0);
        assert!(!app.take_focus_request(), "a drag is movement, not a keyboard request");
    }

    #[test]
    fn wants_keyboard_for_name_and_chat() {
        let mut app = App::new(400, 300, 1.0);
        assert!(app.wants_keyboard()); // name screen
        app.screen = Screen::Play;
        assert!(!app.wants_keyboard());
        app.chat_open = true;
        assert!(app.wants_keyboard());
    }
}
