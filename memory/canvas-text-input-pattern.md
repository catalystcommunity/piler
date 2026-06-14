---
name: canvas-text-input-pattern
description: How piler's canvas-only client accepts text (name/chat) on desktop + mobile
metadata:
  type: project
---

piler's webclient draws ALL UI in the canvas, so text entry uses a hidden
`<textarea>` proxy (the canvas can't take keyboard input or raise a mobile soft
keyboard). The battle-tested pattern (Monaco/CodeMirror/Unity), chosen after the
user rejected several hack attempts:

- **One `<textarea>`** (not `<input>`): correct IME, and its Enter lands a
  detectable newline in `.value` (single-line `<input>` strips newlines; Android
  `enterkeyhint` action key fires NO catchable DOM event — so NO `enterkeyhint`).
- **Invisible via transparency + stacking, NOT opacity hacks.** `color`/
  `background`/`caret-color: transparent`, `outline:0`. Idle: `z-index:-10`
  (behind the opaque canvas) + `pointer-events:none` (canvas keeps drag/
  double-tap gestures). Editing: `z-index:10` + `pointer-events:auto`.
  `font-size:16px` stops iOS focus-zoom. (Earlier `opacity:0.01` left a visible
  speck — do NOT regress to it. Earlier full-screen `100vw/100vh` overlay covered
  the canvas — do NOT regress to it.)
- **Field-overlay positioning:** core exposes `field_rect()` → active box rect in
  CSS px (name box or chat box; `None` during play). Host snaps the textarea
  exactly over that box. Mobile raises the keyboard by a NATIVE tap on the box
  (programmatic `focus()` was unreliable on the user's Android); desktop
  auto-focuses programmatically each frame (`!coarse`).
- **Text** read from the `input` event (`set_text` in core), never keystrokes.
  **Submit** detected by a layered net: `input` value newline + `beforeinput`
  `insertLineBreak` + `keydown` Enter, all funneled through a 150ms-debounced
  `submitEnter()`.

Code: `coreclient/src/app.rs` (`field_rect`, `name_box_rect`, `chat_box_rect`,
`set_text`, `current_text`), `webclient/src/lib.rs` (`fieldRect`/`setText`/
`currentText`), `webclient/web/index.html` (#kbd CSS), `webclient/web/src/main.ts`
(`syncKeyboard`). Key rule: mobile keyboard `focus()` MUST be synchronous in a
user gesture — here it's a native tap on the overlaid box. See [[piler-no-commit]].
