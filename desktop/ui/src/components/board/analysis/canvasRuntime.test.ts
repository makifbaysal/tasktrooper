import { afterEach, describe, expect, it } from "vitest";
import { canvasRuntime } from "@/components/board/analysis/canvasRuntime";

type Sent = Record<string, unknown>;
type Listener = (event: { source?: unknown; data?: unknown }) => void;

// jsdom has no layout, so these cases cover the wiring — what is listened to,
// posted and styled — and leave the geometry to the browser.
function setup(html: string, canvas = true) {
  const iframe = document.createElement("iframe");
  document.body.appendChild(iframe);
  const frameWindow = iframe.contentWindow! as Window & typeof globalThis;
  const doc = iframe.contentDocument!;
  if (canvas) doc.documentElement.setAttribute("data-tt-canvas", "");
  doc.body.innerHTML = html;
  const sent: Sent[] = [];
  const host = { postMessage: (message: Sent) => sent.push(message) };
  const listeners = new Map<string, Listener>();
  const win = {
    document: doc,
    parent: host,
    getSelection: () => frameWindow.getSelection(),
    getComputedStyle: (element: Element) => frameWindow.getComputedStyle(element),
    requestAnimationFrame: (fn: () => void) => {
      fn();
      return 0;
    },
    addEventListener: (type: string, fn: Listener) => listeners.set(type, fn),
  } as unknown as Window;
  canvasRuntime(win);
  const deliver = (data: unknown, source: unknown = host) => listeners.get("message")?.({ source, data });
  const zooms = () => sent.filter((message) => message.type === "tt:zoom").map((message) => message.zoom);
  const pointer = (type: string, x: number, y = 10, target: Element = doc.body) =>
    target.dispatchEvent(
      new frameWindow.MouseEvent(type, { bubbles: true, cancelable: true, button: 0, clientX: x, clientY: y }),
    );
  return { doc, sent, listeners, deliver, zooms, pointer };
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("canvasRuntime", () => {
  it("does nothing in a document that is not a canvas", () => {
    const { sent, listeners } = setup("<p>Analysis</p>", false);
    expect(sent).toEqual([]);
    expect(listeners.size).toBe(0);
  });

  it("reports 100% once installed and steps the body's zoom on the page's request", () => {
    const { doc, deliver, zooms } = setup("<main>Frames</main>");
    expect(zooms()).toEqual([1]);

    deliver({ type: "tt:zoom", action: "in" });
    expect(doc.body.style.getPropertyValue("zoom")).toBe("1.25");
    deliver({ type: "tt:zoom", action: "in" });
    deliver({ type: "tt:zoom", action: "out" });
    deliver({ type: "tt:zoom", action: "out" });
    deliver({ type: "tt:zoom", action: "out" });
    expect(doc.body.style.getPropertyValue("zoom")).toBe("0.9");
    deliver({ type: "tt:zoom", action: "reset" });
    expect(zooms()).toEqual([1, 1.25, 1.5, 1.25, 1, 0.9, 1]);
  });

  it("only takes zoom requests from the page", () => {
    const { deliver, zooms } = setup("<main>Frames</main>");
    deliver({ type: "tt:zoom", action: "in" }, {});
    deliver({ type: "tt:zoom", action: "sideways" });
    expect(zooms()).toEqual([1]);
  });

  it("pans on a drag over empty space and swallows the click that ends it", () => {
    const { doc, pointer } = setup("<main><a href='#x'>link</a></main>");
    let clicks = 0;
    doc.addEventListener("click", () => clicks++);

    pointer("pointerdown", 10);
    expect(doc.documentElement.classList.contains("tt-panning")).toBe(true);
    pointer("pointermove", 60);
    pointer("pointerup", 60);
    expect(doc.documentElement.classList.contains("tt-panning")).toBe(false);

    pointer("click", 60);
    expect(clicks).toBe(0);
    pointer("pointerdown", 10);
    pointer("pointerup", 10);
    pointer("click", 10);
    expect(clicks).toBe(1);
  });

  it("leaves a press in an answer box alone", () => {
    const { doc, pointer } = setup("<textarea>answer</textarea>");
    pointer("pointerdown", 10, 10, doc.querySelector("textarea")!);
    expect(doc.documentElement.classList.contains("tt-panning")).toBe(false);
  });
});
