/**
 * The design frame's pan-and-zoom canvas, installed next to frameRuntime and a
 * no-op unless the srcdoc marked `<html data-tt-canvas>`.
 *
 * A design document is rows of device frames (375, 1440, …) wider than any
 * pane, which the agent's own CSS usually puts in `overflow-x:auto` strips, so
 * the review saw one frame and a scrollbar. Here those strips are opened up,
 * the page itself scrolls both ways with its scrollbars hidden, and it is moved
 * the way Figma moves a canvas: drag empty space (or hold Space, or use the
 * middle button) to pan, trackpad/wheel to scroll, pinch or Ctrl/⌘ + wheel to
 * zoom around the pointer. Dragging over text still selects it, because a
 * selection is how a reviewer comments.
 *
 * Zoom is CSS `zoom` on `<body>` with the body's width frozen at its first
 * layout, so zooming scales the page instead of reflowing it.
 *
 * Page → frame: `tt:zoom` {action: "in" | "out" | "reset" | "fit"}.
 * Frame → page: `tt:zoom` {zoom} after every change and once at install.
 *
 * Serialized with `Function.prototype.toString` (see buildFrameScript), so it
 * must not reach anything outside its own body.
 */
export function canvasRuntime(win: Window): void {
  const doc = win.document;
  const root = doc.documentElement;
  if (!root || !root.hasAttribute("data-tt-canvas")) return;
  const host = win.parent;
  const ZOOM_MIN = 0.1;
  const ZOOM_MAX = 4;
  const ZOOM_STEPS = [0.1, 0.25, 0.33, 0.5, 0.67, 0.75, 0.9, 1, 1.25, 1.5, 2, 3, 4];
  const WHEEL_RATE = 0.01;
  const WHEEL_DELTA_MAX = 25;
  const DRAG_THRESHOLD = 3;
  const FIT_MARGIN = 32;
  const GRAB = "tt-grab";
  const PANNING = "tt-panning";
  const FIELDS = "textarea, input, select, label, [contenteditable]";

  let zoom = 1;
  let contentWidth = 0;
  let space = false;
  let suppressClick = false;
  let hoverQueued = false;
  let hoverX = 0;
  let hoverY = 0;
  let pan: { id: number; x: number; y: number; left: number; top: number; moved: boolean } | null = null;

  const send = (message: Record<string, unknown>) => {
    host.postMessage(message, "*");
  };
  const report = () => send({ type: "tt:zoom", zoom: Math.round(zoom * 1000) / 1000 });
  const scroller = () => doc.scrollingElement || root;
  const clamp = (value: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value));

  const elementOf = (target: EventTarget | null): Element | null => {
    const node = target as Node | null;
    if (!node) return null;
    return node.nodeType === 1 ? (node as Element) : node.parentElement;
  };
  const isField = (target: EventTarget | null) => {
    const element = elementOf(target);
    return element !== null && element.closest(FIELDS) !== null;
  };

  // Text is where a drag selects; anywhere else it pans. The caret APIs return
  // the nearest character even over empty space, so the hit is confirmed
  // against that character's own box.
  const overText = (x: number, y: number): boolean => {
    let node: Node | null = null;
    let offset = 0;
    const d = doc as Document & {
      caretPositionFromPoint?: (x: number, y: number) => { offsetNode: Node; offset: number } | null;
      caretRangeFromPoint?: (x: number, y: number) => Range | null;
    };
    if (typeof d.caretPositionFromPoint === "function") {
      const position = d.caretPositionFromPoint(x, y);
      if (position) {
        node = position.offsetNode;
        offset = position.offset;
      }
    } else if (typeof d.caretRangeFromPoint === "function") {
      const range = d.caretRangeFromPoint(x, y);
      if (range) {
        node = range.startContainer;
        offset = range.startOffset;
      }
    }
    if (!node || node.nodeType !== 3) return false;
    const length = (node.nodeValue || "").length;
    const range = doc.createRange();
    for (let i = Math.max(0, offset - 1); i <= Math.min(length - 1, offset); i++) {
      range.setStart(node, i);
      range.setEnd(node, i + 1);
      const box = range.getBoundingClientRect();
      if (box.width > 0 && x >= box.left - 2 && x <= box.right + 2 && y >= box.top && y <= box.bottom) return true;
    }
    return false;
  };

  // Only page-level strips: a narrow scroller inside a device frame is part of
  // the design (and clipped by the frame anyway), so it keeps its overflow.
  const unclip = () => {
    const body = doc.body;
    if (!body) return;
    const wide = body.clientWidth * 0.6;
    const all = body.querySelectorAll("*");
    for (let i = 0; i < all.length; i++) {
      const element = all[i] as HTMLElement;
      if (element.closest("[data-tt-skip]") || element.matches(FIELDS)) continue;
      const style = win.getComputedStyle(element);
      if (style.overflowX !== "auto" && style.overflowX !== "scroll") continue;
      if (element.scrollWidth <= element.clientWidth + 1) continue;
      if (element.scrollHeight > element.clientHeight + 1) continue;
      if (element.clientWidth < wide && !element.querySelector("figure")) continue;
      element.style.setProperty("overflow", "visible", "important");
      if (element.tagName === "PRE") element.style.setProperty("width", "max-content", "important");
    }
  };

  const freeze = () => {
    const body = doc.body;
    if (!body || zoom !== 1) return;
    body.style.removeProperty("width");
    const width = body.getBoundingClientRect().width;
    if (width > 0) body.style.setProperty("width", width + "px", "important");
    contentWidth = scroller().scrollWidth;
  };

  const layout = () => {
    unclip();
    freeze();
  };

  const setZoom = (next: number, anchorX: number, anchorY: number) => {
    const body = doc.body;
    const target = clamp(next);
    if (!body || Math.abs(target - zoom) < 0.0005) {
      report();
      return;
    }
    const before = body.getBoundingClientRect();
    const bodyX = (anchorX - before.left) / zoom;
    const bodyY = (anchorY - before.top) / zoom;
    zoom = target;
    body.style.setProperty("zoom", String(target));
    const after = body.getBoundingClientRect();
    const s = scroller();
    s.scrollLeft += after.left + bodyX * target - anchorX;
    s.scrollTop += after.top + bodyY * target - anchorY;
    report();
  };

  const step = (direction: 1 | -1) => {
    let next = zoom;
    if (direction > 0) {
      for (let i = 0; i < ZOOM_STEPS.length; i++) {
        if (ZOOM_STEPS[i] > zoom + 0.001) {
          next = ZOOM_STEPS[i];
          break;
        }
      }
    } else {
      for (let i = ZOOM_STEPS.length - 1; i >= 0; i--) {
        if (ZOOM_STEPS[i] < zoom - 0.001) {
          next = ZOOM_STEPS[i];
          break;
        }
      }
    }
    return next;
  };

  const center = () => {
    const s = scroller();
    return { x: s.clientWidth / 2, y: s.clientHeight / 2 };
  };

  const fit = () => {
    const s = scroller();
    if (contentWidth <= 0) return;
    setZoom(Math.min(1, (s.clientWidth - FIT_MARGIN) / contentWidth), 0, 0);
    s.scrollLeft = 0;
  };

  const updateHover = () => {
    hoverQueued = false;
    if (pan) return;
    const hovered = elementOf(doc.elementFromPoint ? doc.elementFromPoint(hoverX, hoverY) : null);
    const grab = space || (!(hovered && hovered.closest(FIELDS)) && !overText(hoverX, hoverY));
    root.classList.toggle(GRAB, grab);
  };

  const endPan = () => {
    if (!pan) return;
    suppressClick = pan.moved;
    if (!pan.moved) {
      const selection = win.getSelection();
      if (selection) selection.removeAllRanges();
    }
    pan = null;
    root.classList.remove(PANNING);
  };

  doc.addEventListener(
    "pointerdown",
    (event) => {
      suppressClick = false;
      const middle = event.button === 1;
      if (!middle && event.button !== 0) return;
      if (!middle && !space && (isField(event.target) || overText(event.clientX, event.clientY))) return;
      event.preventDefault();
      const s = scroller();
      pan = {
        id: event.pointerId,
        x: event.clientX,
        y: event.clientY,
        left: s.scrollLeft,
        top: s.scrollTop,
        moved: false,
      };
      root.classList.add(PANNING);
      try {
        root.setPointerCapture(event.pointerId);
      } catch {
        // A pointer that is already gone; the pan still ends on pointerup.
      }
    },
    true,
  );

  doc.addEventListener("pointermove", (event) => {
    if (pan && event.pointerId === pan.id) {
      const dx = event.clientX - pan.x;
      const dy = event.clientY - pan.y;
      if (!pan.moved && Math.abs(dx) + Math.abs(dy) > DRAG_THRESHOLD) pan.moved = true;
      const s = scroller();
      s.scrollLeft = pan.left - dx;
      s.scrollTop = pan.top - dy;
      return;
    }
    hoverX = event.clientX;
    hoverY = event.clientY;
    if (!hoverQueued) {
      hoverQueued = true;
      win.requestAnimationFrame(updateHover);
    }
  });

  doc.addEventListener("pointerup", endPan);
  doc.addEventListener("pointercancel", endPan);

  // A pan that ends over a mark or a link is not a click on it.
  doc.addEventListener(
    "click",
    (event) => {
      if (!suppressClick) return;
      suppressClick = false;
      event.preventDefault();
      event.stopImmediatePropagation();
    },
    true,
  );

  doc.addEventListener("keydown", (event) => {
    if (event.code !== "Space" || isField(event.target)) return;
    event.preventDefault();
    if (!space) {
      space = true;
      root.classList.add(GRAB);
    }
  });
  doc.addEventListener("keyup", (event) => {
    if (event.code !== "Space") return;
    space = false;
    root.classList.remove(GRAB);
  });
  win.addEventListener("blur", () => {
    space = false;
    root.classList.remove(GRAB);
    endPan();
  });

  // A trackpad pinch arrives as a wheel event with ctrlKey set.
  win.addEventListener(
    "wheel",
    (event) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      const delta = event.deltaMode === 1 ? event.deltaY * 16 : event.deltaY;
      const bounded = Math.max(-WHEEL_DELTA_MAX, Math.min(WHEEL_DELTA_MAX, delta));
      setZoom(zoom * Math.exp(-bounded * WHEEL_RATE), event.clientX, event.clientY);
    },
    { passive: false },
  );

  win.addEventListener("message", (event) => {
    if (event.source !== host) return;
    const data = event.data;
    if (!data || typeof data !== "object" || data.type !== "tt:zoom") return;
    const middle = center();
    if (data.action === "in") setZoom(step(1), middle.x, middle.y);
    else if (data.action === "out") setZoom(step(-1), middle.x, middle.y);
    else if (data.action === "reset") setZoom(1, middle.x, middle.y);
    else if (data.action === "fit") fit();
  });

  win.addEventListener("resize", freeze);
  win.addEventListener("load", layout);
  layout();
  report();
}
