/**
 * The design frame's canvas, installed next to frameRuntime and a no-op unless
 * the srcdoc marked `<html data-tt-canvas>`.
 *
 * A design document is rows of device frames (375, 1440, …) wider than any
 * pane, which the agent's own CSS usually puts in `overflow-x:auto` strips, so
 * the review saw one frame and a scrollbar. Here the document is laid out
 * once at the frame's width, those strips are opened up, and `<body>` becomes
 * a board on an unbounded canvas moved the way Figma moves one — a transform,
 * never scrolling. Drag empty space (or hold Space, or use the middle button)
 * and the trackpad/wheel pan; pinch or Ctrl/⌘ + wheel zooms around the
 * pointer. Dragging over text still selects it, because a selection is how a
 * reviewer comments.
 *
 * Each `<h2>` starts a page — its `<section>` when it has one to itself, else
 * the run of siblings up to the next heading — so the review page can list
 * them, jump to one, and line two variants up page by page.
 *
 * Page → frame: `tt:zoom` {action: "in" | "out" | "reset" | "fit"},
 * `tt:goto` {page}, and `tt:scrollTo` {id}, which frameRuntime leaves to this
 * runtime on a canvas.
 * Frame → page: `tt:zoom` {zoom}, `tt:outline` {pages: [{id, title}]},
 * `tt:page` {id, cause} when the page in view changes — `view` when the
 * reviewer moved the canvas, `goto` when it was moved for them (a goto, the
 * first layout), so a page that follows another never echoes back.
 *
 * Serialized with `Function.prototype.toString` (see buildFrameScript), so it
 * must not reach anything outside its own body.
 */
export function canvasRuntime(win: Window): void {
  const doc = win.document;
  const root = doc.documentElement;
  if (!root || !root.hasAttribute("data-tt-canvas")) return;
  const host = win.parent;
  const ZOOM_MIN = 0.05;
  const ZOOM_MAX = 4;
  const ZOOM_STEPS = [0.1, 0.25, 0.33, 0.5, 0.67, 0.75, 0.9, 1, 1.25, 1.5, 2, 3, 4];
  const WHEEL_RATE = 0.01;
  const WHEEL_DELTA_MAX = 25;
  const DRAG_THRESHOLD = 3;
  const PAD = 32;
  const KEEP_VISIBLE = 96;
  const MAX_PAGES = 100;
  const GRAB = "tt-grab";
  const PANNING = "tt-panning";
  const FIELDS = "textarea, input, select, label, [contenteditable]";

  // Boxes are kept in the body's own, untransformed pixels: the layout never
  // changes after the first pass, so they are measured once, not per frame.
  interface Box {
    left: number;
    top: number;
    right: number;
    bottom: number;
  }
  interface Page {
    id: string;
    title: string;
    box: Box | null;
  }

  let zoom = 1;
  let tx = 0;
  let ty = 0;
  let content: Box | null = null;
  let pages: Page[] = [];
  let currentPage: string | null = null;
  let viewQueued = false;
  let userMoved = false;
  let fitted = false;
  let space = false;
  let suppressClick = false;
  let hoverQueued = false;
  let hoverX = 0;
  let hoverY = 0;
  let pan: { id: number; x: number; y: number; tx: number; ty: number; moved: boolean } | null = null;

  const send = (message: Record<string, unknown>) => {
    host.postMessage(message, "*");
  };
  const clamp = (value: number) => Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, value));
  const viewport = () => ({
    width: root.clientWidth || win.innerWidth || 0,
    height: root.clientHeight || win.innerHeight || 0,
  });
  const report = () => send({ type: "tt:zoom", zoom: Math.round(zoom * 1000) / 1000 });

  const elementOf = (target: EventTarget | null): Element | null => {
    const node = target as Node | null;
    if (!node) return null;
    return node.nodeType === 1 ? (node as Element) : node.parentElement;
  };
  const isField = (target: EventTarget | null) => {
    const element = elementOf(target);
    return element !== null && element.closest(FIELDS) !== null;
  };

  // Where the body's top-left corner is drawn right now, in viewport pixels.
  const origin = () => {
    const body = doc.body;
    if (!body) return { x: 0, y: 0 };
    const r = body.getBoundingClientRect();
    return { x: r.left, y: r.top };
  };

  // Overflowing descendants included — a strip's frames hang outside the
  // strip's own box.
  const measure = (elements: Element[]): Box | null => {
    const o = origin();
    let box: Box | null = null;
    const add = (element: Element) => {
      const r = element.getBoundingClientRect();
      if (r.width === 0 && r.height === 0) return;
      const left = (r.left - o.x) / zoom;
      const top = (r.top - o.y) / zoom;
      const right = (r.right - o.x) / zoom;
      const bottom = (r.bottom - o.y) / zoom;
      if (!box) box = { left, top, right, bottom };
      else {
        box.left = Math.min(box.left, left);
        box.top = Math.min(box.top, top);
        box.right = Math.max(box.right, right);
        box.bottom = Math.max(box.bottom, bottom);
      }
    };
    for (let i = 0; i < elements.length; i++) {
      add(elements[i]);
      const inner = elements[i].querySelectorAll("*");
      for (let j = 0; j < inner.length; j++) add(inner[j]);
    }
    return box;
  };

  const toView = (box: Box): Box => {
    const o = origin();
    return {
      left: o.x + box.left * zoom,
      top: o.y + box.top * zoom,
      right: o.x + box.right * zoom,
      bottom: o.y + box.bottom * zoom,
    };
  };

  const apply = () => {
    const body = doc.body;
    if (!body) return;
    body.style.setProperty("transform", "translate(" + tx + "px," + ty + "px) scale(" + zoom + ")", "important");
    queueView();
  };

  // Never let the board leave the screen entirely: one stray fling should not
  // leave the reviewer staring at empty canvas.
  const keepInView = () => {
    if (!content) return;
    const box = toView(content);
    const v = viewport();
    const keepX = Math.min(KEEP_VISIBLE, (box.right - box.left) / 2);
    const keepY = Math.min(KEEP_VISIBLE, (box.bottom - box.top) / 2);
    let dx = 0;
    let dy = 0;
    if (box.right < keepX) dx = keepX - box.right;
    else if (box.left > v.width - keepX) dx = v.width - keepX - box.left;
    if (box.bottom < keepY) dy = keepY - box.bottom;
    else if (box.top > v.height - keepY) dy = v.height - keepY - box.top;
    if (dx || dy) {
      tx += dx;
      ty += dy;
      apply();
    }
  };

  const panBy = (dx: number, dy: number) => {
    tx += dx;
    ty += dy;
    apply();
    keepInView();
  };

  const zoomAt = (next: number, x: number, y: number) => {
    const target = clamp(next);
    if (Math.abs(target - zoom) < 0.0005) {
      report();
      return;
    }
    const o = origin();
    const localX = (x - o.x) / zoom;
    const localY = (y - o.y) / zoom;
    const staticX = o.x - tx;
    const staticY = o.y - ty;
    zoom = target;
    tx = x - localX * zoom - staticX;
    ty = y - localY * zoom - staticY;
    apply();
    keepInView();
    report();
  };

  // Shows a box (body pixels) centred, at the zoom that fits it but never
  // above `maxZoom`. A box too tall to fit legibly is fitted to the width and
  // shown from its top.
  const frameBox = (box: Box, maxZoom: number) => {
    const v = viewport();
    const width = box.right - box.left;
    const height = box.bottom - box.top;
    if (width <= 0 || height <= 0 || v.width <= 0) return;
    const fitWidth = (v.width - 2 * PAD) / width;
    const fitBoth = Math.min(fitWidth, (v.height - 2 * PAD) / height);
    const tall = fitBoth < 0.25 && fitWidth > fitBoth;
    const o = origin();
    const staticX = o.x - tx;
    const staticY = o.y - ty;
    zoom = clamp(Math.min(maxZoom, tall ? fitWidth : fitBoth));
    tx = (v.width - width * zoom) / 2 - box.left * zoom - staticX;
    ty = (tall || height * zoom > v.height - 2 * PAD ? PAD : (v.height - height * zoom) / 2) - box.top * zoom - staticY;
    apply();
    report();
  };

  const fitWidth = (): boolean => {
    if (!content) return false;
    const v = viewport();
    const width = content.right - content.left;
    if (width <= 0 || v.width <= 0) return false;
    const o = origin();
    const staticX = o.x - tx;
    const staticY = o.y - ty;
    zoom = clamp(Math.min(1, (v.width - 2 * PAD) / width));
    tx = (v.width - width * zoom) / 2 - content.left * zoom - staticX;
    ty = PAD - content.top * zoom - staticY;
    apply();
    report();
    return true;
  };

  const reveal = (element: Element) => {
    const r = element.getBoundingClientRect();
    const v = viewport();
    panBy(v.width / 2 - (r.left + r.width / 2), v.height / 2 - (r.top + r.height / 2));
  };

  // The visible page nearest the middle of the view: zoomed out, several pages
  // are wholly in view and the one the reviewer centred is the one they mean.
  const pageInView = (): string | null => {
    const v = viewport();
    const cx = v.width / 2;
    const cy = v.height / 2;
    let best: string | null = null;
    let bestDistance = Infinity;
    for (const page of pages) {
      if (!page.box) continue;
      const box = toView(page.box);
      if (box.right <= 0 || box.left >= v.width || box.bottom <= 0 || box.top >= v.height) continue;
      const dx = Math.max(box.left - cx, 0, cx - box.right);
      const dy = Math.max(box.top - cy, 0, cy - box.bottom);
      const distance = dx * dx + dy * dy;
      if (distance < bestDistance) {
        bestDistance = distance;
        best = page.id;
      }
    }
    return best;
  };

  const updateView = () => {
    const id = pageInView();
    if (id === currentPage) return;
    currentPage = id;
    send({ type: "tt:page", id, cause: userMoved ? "view" : "goto" });
  };
  const queueView = () => {
    if (viewQueued || pages.length === 0) return;
    viewQueued = true;
    win.requestAnimationFrame(() => {
      viewQueued = false;
      updateView();
    });
  };

  const goTo = (id: string) => {
    const page = pages.find((p) => p.id === id);
    if (!page || !page.box) return;
    userMoved = false;
    frameBox(page.box, 1);
    currentPage = id;
    send({ type: "tt:page", id, cause: "goto" });
  };

  const outline = () => {
    const body = doc.body;
    pages = [];
    if (!body) return;
    const headings = body.querySelectorAll("h2");
    for (let i = 0; i < headings.length && pages.length < MAX_PAGES; i++) {
      const heading = headings[i];
      if (heading.closest("[data-tt-skip]")) continue;
      const title = (heading.textContent || "").replace(/\s+/g, " ").trim().slice(0, 120);
      if (!title) continue;
      const section = heading.closest("section");
      let elements: Element[];
      if (section && section.querySelectorAll("h2").length === 1) {
        elements = [section];
      } else {
        elements = [heading];
        let next = heading.nextElementSibling;
        while (next && next.tagName !== "H2" && !next.querySelector("h2")) {
          elements.push(next);
          next = next.nextElementSibling;
        }
      }
      pages.push({ id: "p" + pages.length, title, box: measure(elements) });
    }
    send({ type: "tt:outline", pages: pages.map((p) => ({ id: p.id, title: p.title })) });
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

  // Laid out once at the frame's width; the transform only moves and scales
  // that layout, so nothing reflows while the reviewer pans or zooms.
  let laidOut = false;
  const layout = () => {
    const body = doc.body;
    if (!body) return;
    if (!laidOut) {
      const width = body.getBoundingClientRect().width / zoom;
      if (width > 0) {
        body.style.setProperty("width", width + "px", "important");
        laidOut = true;
      }
    }
    unclip();
    content = measure([body]);
    outline();
    if (!fitted) fitted = fitWidth();
    userMoved = false;
    currentPage = null;
    queueView();
  };

  const step = (direction: 1 | -1) => {
    if (direction > 0) {
      for (let i = 0; i < ZOOM_STEPS.length; i++) if (ZOOM_STEPS[i] > zoom + 0.001) return ZOOM_STEPS[i];
      return ZOOM_MAX;
    }
    for (let i = ZOOM_STEPS.length - 1; i >= 0; i--) if (ZOOM_STEPS[i] < zoom - 0.001) return ZOOM_STEPS[i];
    return ZOOM_MIN;
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
    keepInView();
  };

  doc.addEventListener(
    "pointerdown",
    (event) => {
      suppressClick = false;
      const middle = event.button === 1;
      if (!middle && event.button !== 0) return;
      if (!middle && !space && (isField(event.target) || overText(event.clientX, event.clientY))) return;
      event.preventDefault();
      pan = { id: event.pointerId, x: event.clientX, y: event.clientY, tx, ty, moved: false };
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
      userMoved = true;
      tx = pan.tx + dx;
      ty = pan.ty + dy;
      apply();
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

  // A pan that ends over a mark or a link is not a click on it; an in-page
  // link moves the canvas, since the frame never scrolls.
  doc.addEventListener(
    "click",
    (event) => {
      if (suppressClick) {
        suppressClick = false;
        event.preventDefault();
        event.stopImmediatePropagation();
        return;
      }
      const element = elementOf(event.target);
      const link = element ? element.closest("a[href]") : null;
      const href = link ? link.getAttribute("href") || "" : "";
      if (href.charAt(0) !== "#" || href.length < 2) return;
      let destination: Element | null = null;
      try {
        destination = doc.getElementById(decodeURIComponent(href.slice(1)));
      } catch {
        destination = null;
      }
      if (destination) reveal(destination);
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

  // A trackpad pinch arrives as a wheel event with ctrlKey set; a plain wheel
  // or a two-finger swipe pans, as on any canvas.
  win.addEventListener(
    "wheel",
    (event) => {
      event.preventDefault();
      userMoved = true;
      const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewport().height : 1;
      if (event.ctrlKey || event.metaKey) {
        const bounded = Math.max(-WHEEL_DELTA_MAX, Math.min(WHEEL_DELTA_MAX, event.deltaY * unit));
        zoomAt(zoom * Math.exp(-bounded * WHEEL_RATE), event.clientX, event.clientY);
        return;
      }
      let dx = event.deltaX * unit;
      let dy = event.deltaY * unit;
      if (event.shiftKey && dx === 0) {
        dx = dy;
        dy = 0;
      }
      panBy(-dx, -dy);
    },
    { passive: false },
  );

  // The frame never scrolls; whatever scrolled it anyway — focusing an answer
  // box, the document's own scrollIntoView — becomes a pan.
  win.addEventListener("scroll", () => {
    const sx = win.scrollX || 0;
    const sy = win.scrollY || 0;
    if (!sx && !sy) return;
    win.scrollTo(0, 0);
    panBy(-sx, -sy);
  });

  win.addEventListener("message", (event) => {
    if (event.source !== host) return;
    const data = event.data;
    if (!data || typeof data !== "object") return;
    if (data.type === "tt:zoom") {
      userMoved = true;
      const v = viewport();
      if (data.action === "in") zoomAt(step(1), v.width / 2, v.height / 2);
      else if (data.action === "out") zoomAt(step(-1), v.width / 2, v.height / 2);
      else if (data.action === "reset") zoomAt(1, v.width / 2, v.height / 2);
      else if (data.action === "fit") fitWidth();
    } else if (data.type === "tt:goto" && typeof data.page === "string") {
      goTo(data.page);
    } else if (data.type === "tt:scrollTo" && typeof data.id === "string") {
      const marks = doc.querySelectorAll("mark[data-tt-id]");
      for (let i = 0; i < marks.length; i++) {
        if (marks[i].getAttribute("data-tt-id") === data.id) {
          reveal(marks[i]);
          break;
        }
      }
    }
  });

  // The frame can be parsed before its iframe has a size (a dialog still
  // opening): the first real layout, and the first fit, wait for one.
  win.addEventListener("load", layout);
  win.addEventListener("resize", () => {
    if (!laidOut || !fitted) layout();
  });
  report();
  layout();
}
