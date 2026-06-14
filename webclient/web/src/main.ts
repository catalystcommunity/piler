// piler browser host — intentionally tiny.
//
// Everything (UI, rendering, input handling, CBOR, state) lives in the WASM
// core. This file only:
//   1. establishes the WebSocket and tunnels bytes both ways,
//   2. forwards keyboard events to the core,
//   3. blits the core's framebuffer to the canvas (putImageData) at ~30fps.

import init, { App } from "../wasm/piler.js";
import wasmUrl from "../wasm/piler_bg.wasm?url";

const FPS = 30;

const wasm = await init({ module_or_path: wasmUrl });
const canvas = document.getElementById("screen") as HTMLCanvasElement;
const ctx = canvas.getContext("2d")!;

// Render at device resolution: the backing store is css * devicePixelRatio,
// and the WASM draws at scale = dpr so the field keeps a consistent ratio to
// the monitor's pixel density (e.g. 1 logical px = 2x2 device px on a 4K/HiDPI
// display). The canvas is displayed at css size.
function dims() {
  const dpr = window.devicePixelRatio || 1;
  return {
    dpr,
    w: Math.round(window.innerWidth * dpr),
    h: Math.round(window.innerHeight * dpr),
  };
}
const d0 = dims();
const app = new App(d0.w, d0.h, d0.dpr);

function resize() {
  const d = dims();
  canvas.width = d.w;
  canvas.height = d.h;
  canvas.style.width = window.innerWidth + "px";
  canvas.style.height = window.innerHeight + "px";
  app.resize(d.w, d.h, d.dpr);
}
resize();
window.addEventListener("resize", resize);

// --- WebSocket tunnel (the only networking; no protocol logic here) ---
const ws = new WebSocket(`ws://${location.hostname}:6080/ws`);
ws.binaryType = "arraybuffer";
ws.onmessage = (e) => app.receive(new Uint8Array(e.data as ArrayBuffer));

// --- text entry via the hidden textarea ---
// The textarea's value is the source of typed text. When the core is in a
// text-entry state it's expanded to full-screen (pointer-events:auto) so a tap
// focuses it NATIVELY — the only reliable way to raise a mobile keyboard.
const kbd = document.getElementById("kbd") as HTMLTextAreaElement;
const coarse = window.matchMedia("(pointer: coarse)").matches;

// Enter can arrive several ways across keyboards (keydown, a beforeinput
// line-break, or a newline that lands in the value). Funnel them all through
// one debounced path so a single press doesn't double-fire.
let lastEnter = 0;
function submitEnter() {
  const now = performance.now();
  if (now - lastEnter < 150) return;
  lastEnter = now;
  app.keyDown("Enter", false);
}

// Push the field's value into the core, treating any newline as a submit and
// keeping it out of the stored text.
function syncText() {
  if (/[\r\n]/.test(kbd.value)) {
    kbd.value = kbd.value.replace(/[\r\n]+/g, "");
    app.setText(kbd.value);
    submitEnter();
  } else {
    app.setText(kbd.value);
  }
}
kbd.addEventListener("input", syncText);
// Some keyboards signal Enter as a line-break intent rather than a value change.
kbd.addEventListener("beforeinput", (e) => {
  const t = (e as InputEvent).inputType;
  if (t === "insertLineBreak" || t === "insertParagraph") {
    e.preventDefault();
    submitEnter();
  }
});

// Snap the textarea over the active on-canvas box and manage focus. The core
// reports the box rect (CSS px) only while entering text; otherwise the field
// parks behind the canvas and we blur it (dismissing the mobile keyboard).
let lastRect = "";
function syncKeyboard() {
  const r = app.fieldRect(); // [x, y, w, h] or []
  const editing = r.length === 4;

  if (editing) {
    if (kbd.className !== "editing") {
      kbd.className = "editing";
      kbd.value = app.currentText(); // seed before native-tap typing
      lastRect = "";
    }
    const key = `${r[0]},${r[1]},${r[2]},${r[3]}`;
    if (key !== lastRect) {
      kbd.style.left = `${r[0]}px`;
      kbd.style.top = `${r[1]}px`;
      kbd.style.width = `${r[2]}px`;
      kbd.style.height = `${r[3]}px`;
      lastRect = key;
    }
    // Mobile focuses by a native tap on the box (the textarea now sits there);
    // desktop has no tap, so focus programmatically to type immediately.
    if (!coarse && document.activeElement !== kbd) {
      kbd.value = app.currentText();
      kbd.focus();
    }
  } else {
    if (kbd.className !== "idle") {
      kbd.className = "idle";
      kbd.style.left = "0px";
      kbd.style.top = "0px";
      kbd.style.width = "1px";
      kbd.style.height = "1px";
      lastRect = "";
    }
    // Keep focus through the pending-tap window (the keyboard was raised in the
    // tap gesture and chat is about to open); only blur once nothing wants it.
    if (!app.wantsKeyboard() && document.activeElement === kbd) kbd.blur();
  }
}

// --- keyboard for game control (Enter/Escape/movement/space) ---
window.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault(); // keep the textarea from inserting a newline too
    submitEnter();
    return;
  }
  // While typing (name/chat) let the textarea work; otherwise prevent default
  // so movement/space/arrows don't scroll the page.
  if (!app.wantsKeyboard() && !e.ctrlKey && !e.metaKey && !e.altKey) e.preventDefault();
  app.keyDown(e.key, e.repeat);
});
window.addEventListener("keyup", (e) => app.keyUp(e.key));

// --- touch input: drag = 8-way stick, double-tap = firework, tap = chat ---
// During play the textarea is .idle (behind the canvas, pointer-events:none) so
// these fire. While entering text it sits over the input box and a tap there
// focuses it natively — that's what raises the mobile keyboard.
const isTouch = (e: PointerEvent) => e.pointerType === "touch" || e.pointerType === "pen";
canvas.addEventListener("pointerdown", (e) => {
  if (isTouch(e)) app.pointerDown(e.clientX, e.clientY);
});
canvas.addEventListener("pointermove", (e) => {
  if (isTouch(e)) app.pointerMove(e.clientX, e.clientY);
});
function endPointer(e: PointerEvent) {
  if (!isTouch(e)) return;
  app.pointerUp(e.clientX, e.clientY);
  // A tap that opens chat asks for the keyboard: focus inside this gesture
  // (the only moment mobile honors focus()). The field is still idle/behind the
  // canvas here; it repositions over the chat box when chat actually opens a few
  // frames later. Best-effort — tapping the box itself remains the sure way.
  if (app.takeFocusRequest()) {
    kbd.value = app.currentText();
    kbd.focus();
  }
}
canvas.addEventListener("pointerup", endPointer);
canvas.addEventListener("pointercancel", endPointer);

// --- render + send loop ---
function frame() {
  app.render();
  syncKeyboard(); // desktop auto-focus; hide once name/chat is done

  if (ws.readyState === WebSocket.OPEN) {
    const out = app.drainOutbound();
    for (let i = 0; i < out.length; i++) ws.send(out[i] as Uint8Array);
  }

  // Zero-copy view over the framebuffer in WASM memory (recreated each frame
  // since the memory buffer can move when WASM allocates/resizes).
  const ptr = app.framePtr();
  const len = app.frameLen();
  const view = new Uint8ClampedArray(wasm.memory.buffer, ptr, len);
  ctx.putImageData(new ImageData(view, app.width(), app.height()), 0, 0);
}
setInterval(frame, 1000 / FPS);
