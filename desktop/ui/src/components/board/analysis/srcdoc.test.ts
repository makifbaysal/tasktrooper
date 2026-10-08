import { describe, expect, it } from "vitest";
import {
  buildAnalysisSrcdoc,
  createNonce,
  FRAME_SANDBOX,
  frameCsp,
  markdownFrameHtml,
  OPEN_QUESTIONS_ID,
  parseFrameMessage,
} from "@/components/board/analysis/srcdoc";

function parse(srcdoc: string): Document {
  return new DOMParser().parseFromString(srcdoc, "text/html");
}

const HOSTILE = `<!DOCTYPE html>
<html>
<head>
  <meta http-equiv="Content-Security-Policy" content="default-src *; script-src 'unsafe-inline'">
  <meta http-equiv="refresh" content="0; url=https://example.com/">
  <base href="https://example.com/">
  <link rel="stylesheet" href="https://example.com/x.css">
  <script>window.stolen = document.cookie</script>
</head>
<body>
  <h1>analiz: checkout</h1>
  <noscript><p title="</noscript><img src=x onerror=alert(1)>">x</p></noscript>
  <script nonce="guessed">evil()</script>
  <p onclick="evil()">Body</p>
</body>
</html>`;

describe("FRAME_SANDBOX", () => {
  it("allows the runtime to run but never gives the frame this origin", () => {
    expect(FRAME_SANDBOX.split(/\s+/)).toEqual(["allow-scripts"]);
    expect(FRAME_SANDBOX).not.toContain("allow-same-origin");
  });
});

describe("frameCsp", () => {
  it("blocks everything but inline styles, data/https images, data fonts and the nonce'd script", () => {
    expect(frameCsp("abc123")).toBe(
      "default-src 'none'; style-src 'unsafe-inline'; img-src data: https:; font-src data:; " +
        "script-src 'nonce-abc123'; form-action 'none'; base-uri 'none'",
    );
  });
});

describe("createNonce", () => {
  it("is 128 random bits in hex, different every time", () => {
    const a = createNonce();
    expect(a).toMatch(/^[0-9a-f]{32}$/);
    expect(createNonce()).not.toBe(a);
  });
});

describe("buildAnalysisSrcdoc", () => {
  const srcdoc = buildAnalysisSrcdoc(HOSTILE, { nonce: "n0nce", script: "runtime();" });
  const doc = parse(srcdoc);

  it("puts our CSP first in <head>, ahead of every script", () => {
    const first = doc.head.firstElementChild!;
    expect(first.tagName).toBe("META");
    expect(first.getAttribute("http-equiv")).toBe("Content-Security-Policy");
    expect(first.getAttribute("content")).toBe(frameCsp("n0nce"));
    expect(doc.querySelector("meta, script")).toBe(first);
    expect(srcdoc.startsWith("<!DOCTYPE html><html><head><meta http-equiv=\"Content-Security-Policy\"")).toBe(true);
  });

  it("drops the document's own CSP, refresh, base, link and noscript elements", () => {
    expect(doc.querySelectorAll("meta[http-equiv]")).toHaveLength(1);
    expect(doc.querySelector("base, link, noscript")).toBeNull();
    expect(srcdoc).not.toContain("onerror");
    expect(srcdoc).not.toContain("example.com/x.css");
  });

  it("leaves the document's scripts in place without a nonce, so the policy refuses them", () => {
    const scripts = Array.from(doc.querySelectorAll("script"));
    expect(scripts.map((s) => s.textContent)).toEqual(["window.stolen = document.cookie", "evil()", "runtime();"]);
    expect(doc.querySelectorAll("[nonce]")).toHaveLength(1);
    expect(scripts[0].hasAttribute("nonce")).toBe(false);
    expect(scripts[1].hasAttribute("nonce")).toBe(false);
  });

  it("appends the runtime, carrying the nonce, as the last element of <body>", () => {
    const runtime = doc.body.lastElementChild!;
    expect(runtime.tagName).toBe("SCRIPT");
    expect(runtime.getAttribute("nonce")).toBe("n0nce");
    expect(runtime.textContent).toBe("runtime();");
  });

  it("adds the highlight styles and keeps the document's content", () => {
    expect(doc.head.lastElementChild?.tagName).toBe("STYLE");
    expect(doc.head.lastElementChild?.textContent).toContain("mark[data-tt-id]");
    expect(doc.querySelector("h1")?.textContent).toBe("analiz: checkout");
  });

  it("gives a bare fragment a head to carry the policy", () => {
    const fragment = parse(buildAnalysisSrcdoc("<h1>Only a heading</h1>", { nonce: "n", script: "" }));
    expect(fragment.head.firstElementChild?.getAttribute("content")).toBe(frameCsp("n"));
    expect(fragment.querySelector("h1")?.textContent).toBe("Only a heading");
  });

  it("marks the document as a canvas only when asked", () => {
    expect(doc.documentElement.hasAttribute("data-tt-canvas")).toBe(false);
    expect(srcdoc).not.toContain("tt-panning");

    const canvas = buildAnalysisSrcdoc("<main>Frames</main>", { nonce: "n", script: "", canvas: true });
    const canvasDoc = parse(canvas);
    expect(canvasDoc.documentElement.hasAttribute("data-tt-canvas")).toBe(true);
    expect(canvasDoc.head.querySelector("style")?.textContent).toContain("html[data-tt-canvas].tt-panning");
    expect(canvasDoc.querySelector("main")?.textContent).toBe("Frames");
  });
});

describe("markdownFrameHtml", () => {
  it("wraps rendered markdown in a themed page", () => {
    expect(markdownFrameHtml("<p>x</p>", "dark")).toContain('<html class="dark">');
    expect(markdownFrameHtml("<p>x</p>", "light")).toContain("<main><p>x</p></main>");
  });
});

describe("parseFrameMessage", () => {
  it("accepts and rebuilds the four frame messages", () => {
    expect(parseFrameMessage({ type: "tt:ready", extra: 1 })).toEqual({ type: "tt:ready" });
    expect(
      parseFrameMessage({ type: "tt:selection", quote: "q", prefix: "p", suffix: "s", rect: { top: 1, left: "x" } }),
    ).toEqual({
      type: "tt:selection",
      quote: "q",
      prefix: "p",
      suffix: "s",
      rect: { top: 1, left: 0, width: 0, height: 0 },
    });
    expect(parseFrameMessage({ type: "tt:focus", id: "a1" })).toEqual({ type: "tt:focus", id: "a1" });
    expect(
      parseFrameMessage({ type: "tt:anchored", results: [{ id: "a1", found: true }, { id: 3, found: true }, null] }),
    ).toEqual({ type: "tt:anchored", results: [{ id: "a1", found: true }] });
  });

  it("rejects anything else", () => {
    expect(parseFrameMessage(null)).toBeNull();
    expect(parseFrameMessage("tt:ready")).toBeNull();
    expect(parseFrameMessage({ type: "tt:unknown" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:selection", quote: "  ", prefix: "", suffix: "" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:selection", quote: "q", prefix: "x".repeat(201), suffix: "" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:focus", id: "" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:anchored", results: "all" })).toBeNull();
  });

  it("accepts a well-formed tt:answer, including an empty answer that reopens the question", () => {
    expect(parseFrameMessage({ type: "tt:answer", id: "q1", text: "Ship it" })).toEqual({
      type: "tt:answer",
      id: "q1",
      text: "Ship it",
    });
    expect(parseFrameMessage({ type: "tt:answer", id: "q1", text: "" })).toEqual({
      type: "tt:answer",
      id: "q1",
      text: "",
    });
  });

  it("rejects a tt:answer with a bad id or an over-long answer", () => {
    expect(parseFrameMessage({ type: "tt:answer", id: "", text: "x" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:answer", id: 42, text: "x" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:answer", id: "q1", text: 42 })).toBeNull();
    expect(parseFrameMessage({ type: "tt:answer", id: "q1", text: "x".repeat(4001) })).toBeNull();
    expect(parseFrameMessage({ type: "tt:answer", id: "q1", text: "x".repeat(4000) })).toEqual({
      type: "tt:answer",
      id: "q1",
      text: "x".repeat(4000),
    });
  });

  it("accepts a canvas zoom report only as a plausible number", () => {
    expect(parseFrameMessage({ type: "tt:zoom", zoom: 0.5, extra: true })).toEqual({ type: "tt:zoom", zoom: 0.5 });
    expect(parseFrameMessage({ type: "tt:zoom", zoom: "1" })).toBeNull();
    expect(parseFrameMessage({ type: "tt:zoom", zoom: Number.NaN })).toBeNull();
    expect(parseFrameMessage({ type: "tt:zoom", zoom: 0 })).toBeNull();
    expect(parseFrameMessage({ type: "tt:zoom", zoom: 1000 })).toBeNull();
  });
});

describe("Open questions container", () => {
  it("always adds an empty, hidden, skip-marked section as the first element of <body>", () => {
    const srcdoc = buildAnalysisSrcdoc("<p>Report body</p>", { nonce: "n", script: "" });
    const doc = parse(srcdoc);
    const section = doc.body.firstElementChild!;
    expect(section.id).toBe(OPEN_QUESTIONS_ID);
    expect(section.hasAttribute("data-tt-skip")).toBe(true);
    expect(section.hasAttribute("hidden")).toBe(true);
    expect(section.textContent).toBe("");
    expect(doc.querySelector("p")?.textContent).toBe("Report body");
  });
});
