//! The framebuffer rendering path for [`App`]: every `draw_*` method plus the
//! small drawing helpers. Pulled out of `app/mod.rs` so the read-mostly render
//! code is separately reviewable from the app's state machine and input
//! handling. These are a second `impl App` block — App, its fields, and the
//! shared constants all live in the parent `app` module.

use super::*;
use crate::identicon::Identicon;
use crate::Framebuffer;

impl App {
    // --- rendering ---

    pub(super) fn draw_name(&mut self) {
        self.fb.clear(BG);
        let w = self.fb.width() as i32;
        let h = self.fb.height() as i32;
        let s = self.ui_scale();

        let ts = s + 1;
        let title = "Name?";
        let tx = (w - Framebuffer::text_width(title, ts)) / 2;
        self.fb.draw_text(tx, h / 2 - 16 * ts, title, ts, TEXT);

        let (bx, by, box_w, box_h) = self.name_box_rect();
        self.fb.fill_rect(bx, by, box_w, box_h, PANEL);
        self.fb.stroke_rect(bx, by, box_w, box_h, 2, BORDER);

        let mut shown = self.name.clone();
        if self.cursor_on() {
            shown.push('_');
        }
        self.fb.draw_text(bx + 4 * s, by + 3 * s, &shown, s, TEXT);

        let msg_y = by + box_h + 6 * s;
        if self.name_taken {
            self.fb.draw_text(bx, msg_y, "Sorry, username already in use", s, RED);
        } else {
            self.fb.draw_text(bx, msg_y, "3+ characters, Enter to join  (tap to type)", s, MUTED);
        }
    }

    pub(super) fn draw_play(&mut self) {
        let room = match self.client.room() {
            Some(r) => r.clone(),
            None => {
                self.fb.clear(BG);
                return;
            }
        };
        let me_id = self.client.me().map(|m| m.player_id.clone());

        self.fb.clear(BG);

        let dev_w = self.fb.width() as f64;
        let dev_h = self.fb.height() as f64;
        let scale = self.dpr;
        let view_w = dev_w / scale; // logical px visible
        let view_h = dev_h / scale;
        let field_lw = room.field_w as f64 * LPP;
        let field_lh = room.field_h as f64 * LPP;

        // Camera centered on the player, clamped to the field (or the field
        // centered if it's smaller than the viewport).
        let (mlx, mly) = self.me_logical().unwrap_or((field_lw / 2.0, field_lh / 2.0));
        let cam_x = camera(mlx, view_w, field_lw);
        let cam_y = camera(mly, view_h, field_lh);
        let to_dev = move |lx: f64, ly: f64| -> (i32, i32) {
            (
                ((lx - cam_x) * scale).round() as i32,
                ((ly - cam_y) * scale).round() as i32,
            )
        };

        // field border + grid (only the visible tile lines)
        let (fx0, fy0) = to_dev(0.0, 0.0);
        let (fx1, fy1) = to_dev(field_lw, field_lh);
        let max_kx = (field_lw / PX_PER_TILE) as i64;
        let max_ky = (field_lh / PX_PER_TILE) as i64;
        let k0x = ((cam_x / PX_PER_TILE).floor() as i64).max(0);
        let k1x = (((cam_x + view_w) / PX_PER_TILE).ceil() as i64).min(max_kx);
        for k in k0x..=k1x {
            let (dx, _) = to_dev(k as f64 * PX_PER_TILE, 0.0);
            self.fb.fill_rect(dx, fy0.max(0), 1, fy1 - fy0, GRID);
        }
        let k0y = ((cam_y / PX_PER_TILE).floor() as i64).max(0);
        let k1y = (((cam_y + view_h) / PX_PER_TILE).ceil() as i64).min(max_ky);
        for k in k0y..=k1y {
            let (_, dy) = to_dev(0.0, k as f64 * PX_PER_TILE);
            self.fb.fill_rect(fx0.max(0), dy, fx1 - fx0, 1, GRID);
        }
        let bt = (self.dpr.round() as i32).max(1);
        self.fb.stroke_rect(fx0, fy0, fx1 - fx0, fy1 - fy0, bt, BORDER);

        // players
        let cell = (((AVATAR_LOGICAL * scale) / 5.0).round() as i32).max(1);
        let icon = cell * 5;
        let ns = (self.ui_scale() - 1).max(1);
        for p in &room.players {
            let lx = (p.pos.tile_x as f64 * SUBF + p.pos.sub_x as f64) * LPP;
            let ly = (p.pos.tile_y as f64 * SUBF + p.pos.sub_y as f64) * LPP;
            let (px, py) = to_dev(lx, ly);
            let av = self.avatar(&p.player_id);
            draw_identicon(&mut self.fb, &av, px - icon / 2, py - icon / 2, cell);
            if Some(&p.player_id) == me_id.as_ref() {
                self.fb
                    .stroke_rect(px - icon / 2 - 2, py - icon / 2 - 2, icon + 4, icon + 4, bt, ME);
            }
            let nx = px - Framebuffer::text_width(&p.name, ns) / 2;
            self.fb.draw_text(nx, py - icon / 2 - 10 * ns, &p.name, ns, TEXT);
        }

        // firework (world space)
        self.draw_firework(&to_dev);

        // --- screen-space overlays ---
        let s = self.ui_scale();
        self.fb
            .draw_text(8, 8, "WASD move  -  Enter chat  -  SPACE firework  -  /demo [3-8]", s, MUTED);
        self.draw_player_list(&room.players, me_id.as_ref(), s);
        self.draw_chat(&room.recent_chat, s);
        self.draw_cooldown(s);
        if self.chat_open {
            self.draw_chat_input(s);
        }
    }

    fn draw_firework<F: Fn(f64, f64) -> (i32, i32)>(&mut self, to_dev: &F) {
        // Drop finished fireworks, then draw the rest (everyone's, not just ours).
        let now = self.frame;
        self.fireworks
            .retain(|fw| (now - fw.start) as f32 <= FIRE_DUR);
        if self.fireworks.is_empty() {
            return;
        }
        let psize = (2.0 * self.dpr).round().max(1.0) as i32;
        let spread = self.dpr as f32 * 3.0;
        // Index loop + per-element copy (Firework is Copy) so we don't hold a
        // borrow of self.fireworks while mutably borrowing self.fb below.
        for i in 0..self.fireworks.len() {
            let fw = self.fireworks[i];
            let age = (now - fw.start) as f32;
            let (ox, oy) = to_dev(fw.x, fw.y);
            let fade = 1.0 - age / FIRE_DUR;
            for i in 0..FIRE_PARTICLES {
                let ang = i as f32 / FIRE_PARTICLES as f32 * std::f32::consts::TAU;
                let dx = ang.cos() * spread * age;
                let dy = ang.sin() * spread * age + 0.18 * spread * age * age * 0.1; // gravity
                let color = FIRE_COLORS[(i as usize) % FIRE_COLORS.len()];
                let cx = ox + dx as i32;
                let cy = oy + dy as i32;
                for yy in 0..psize {
                    for xx in 0..psize {
                        self.fb.blend_px(cx + xx, cy + yy, color, fade);
                    }
                }
            }
        }
    }

    fn draw_player_list(&mut self, players: &[crate::csil::Player], me_id: Option<&String>, s: i32) {
        let w = self.fb.width() as i32;
        // Stable order (the server snapshot order is unspecified).
        let mut list = players.to_vec();
        list.sort_by(|a, b| a.player_id.cmp(&b.player_id));

        let pad = 4 * s;
        let line_h = 10 * s;
        // Cap the width, but never below the minimum: a high-dpr `s` can push
        // the scaled min above a fixed cap, and `i32::clamp` PANICS when
        // min > max (which froze the whole client on high-density phones).
        let min_w = 180 * s / 2;
        let panel_w = (w / 4).clamp(min_w, 520.max(min_w));
        let px = w - panel_w - 8;
        let py = 8;
        let panel_h = line_h * (list.len() as i32 + 1) + pad * 2;
        self.fb.fill_rect(px, py, panel_w, panel_h, PANEL);
        self.fb.stroke_rect(px, py, panel_w, panel_h, 1, BORDER);
        self.fb.draw_text(px + pad, py + pad, "players", s, MUTED);
        let swatch = (2 * s).max(2);
        for (i, p) in list.iter().enumerate() {
            let ry = py + pad + line_h * (i as i32 + 1);
            let av = self.avatar(&p.player_id);
            draw_identicon(&mut self.fb, &av, px + pad, ry, swatch);
            let color = if Some(&p.player_id) == me_id { ME } else { TEXT };
            self.fb
                .draw_text(px + pad + swatch * 5 + 4, ry, &p.name, s, color);
        }
    }

    fn draw_chat(&mut self, chat: &[crate::csil::ChatMessage], s: i32) {
        let w = self.fb.width() as i32;
        let h = self.fb.height() as i32;
        let pad = 4 * s;
        let line_h = 10 * s;
        let panel_w = (w / 3).clamp(240, 700);
        let max_lines = 8usize;
        let shown: Vec<&crate::csil::ChatMessage> =
            chat.iter().rev().take(max_lines).rev().collect();
        let panel_h = line_h * (shown.len() as i32 + 1) + pad * 2;
        let px = w - panel_w - 8;
        let bottom = if self.chat_open { line_h + 20 } else { 8 };
        let py = h - panel_h - bottom;
        self.fb.fill_rect(px, py, panel_w, panel_h, PANEL);
        self.fb.stroke_rect(px, py, panel_w, panel_h, 1, BORDER);
        self.fb.draw_text(px + pad, py + pad, "chat", s, MUTED);
        for (i, m) in shown.iter().enumerate() {
            let ry = py + pad + line_h * (i as i32 + 1);
            let line = format!("<{}> {}", m.name, m.message);
            self.fb
                .draw_text(px + pad, ry, &truncate(&line, panel_w - pad * 2, s), s, TEXT);
        }
    }

    fn draw_chat_input(&mut self, s: i32) {
        let w = self.fb.width() as i32;
        let (bx, by, bw, box_h) = self.chat_box_rect();
        self.fb.fill_rect(bx, by, bw, box_h, PANEL);
        self.fb.stroke_rect(bx, by, bw, box_h, 2, ME);
        let mut line = format!("> {}", self.chat);
        if self.cursor_on() {
            line.push('_');
        }
        self.fb
            .draw_text(10, by + 2 * s, &truncate(&line, w - 24, s), s, TEXT);
    }

    fn draw_cooldown(&mut self, s: i32) {
        let h = self.fb.height() as i32;
        let r = 9 * s;
        let cx = 14 + r;
        let cy = h - 18 - r - 8 * s;
        let progress = 1.0 - (self.cooldown as f32 / COOLDOWN_FRAMES as f32);
        self.fb.fill_circle_aa(cx, cy, r, [40, 40, 62]); // track
        let fr = (r as f32 * progress).round() as i32;
        if fr > 0 {
            let col = if progress >= 1.0 {
                [108, 204, 255]
            } else {
                [90, 120, 180]
            };
            self.fb.fill_circle_aa(cx, cy, fr, col);
        }
        let ls = (s - 1).max(1);
        let label = "[space]";
        let lx = cx - Framebuffer::text_width(label, ls) / 2;
        self.fb.draw_text(lx, cy + r + 4, label, ls, MUTED);
    }
}

fn draw_identicon(fb: &mut Framebuffer, ic: &Identicon, x: i32, y: i32, cell: i32) {
    let n = ic.size as i32;
    let color = [ic.color[0], ic.color[1], ic.color[2], 255];
    for row in 0..n {
        for col in 0..n {
            if ic.cells[(row * n + col) as usize] {
                fb.fill_rect(x + col * cell, y + row * cell, cell, cell, color);
            }
        }
    }
}

fn truncate(text: &str, max_px: i32, scale: i32) -> String {
    let max_chars = (max_px / (8 * scale)).max(1) as usize;
    if text.chars().count() <= max_chars {
        return text.to_string();
    }
    text.chars().take(max_chars.saturating_sub(1)).collect::<String>() + "…"
}
