import { canvasRuntime } from "@/components/board/analysis/canvasRuntime";
import { textQuoteKit, type TextQuoteKit, type TextSpan } from "@/components/board/analysis/textQuote";

/**
 * The only script that runs inside the sandboxed analysis frame; everything
 * the document itself carries is blocked by the frame's CSP. It reports the
 * reader's text selections as text-quote selectors, draws the annotations the
 * page sends it as `<mark>`s, and does nothing else — it has no network access
 * and sees nothing of the app but the messages below.
 *
 * Frame → page: `tt:ready`, `tt:selection` {quote, prefix, suffix, rect},
 * `tt:anchored` {results: [{id, found}]}, `tt:focus` {id}, `tt:answer` {id,
 * text} (an Open questions answer box, on blur and on a 1s debounce).
 * Page → frame: `tt:annotations` {items: [{id, quote, prefix, suffix, status,
 * active}]}, `tt:scrollTo` {id}, `tt:questions` {labels, items: [{id, key,
 * kind, blocking, prompt, recommendedAnswer, answer, status, editable}]}.
 *
 * Serialized with `Function.prototype.toString` (see buildFrameScript), so,
 * like textQuoteKit, it must not reach anything outside its own body.
 */
export function frameRuntime(kit: TextQuoteKit, win: Window): void {
  const doc = win.document;
  // A design canvas never scrolls: canvasRuntime moves it to links and marks.
  const canvas = doc.documentElement !== null && doc.documentElement.hasAttribute("data-tt-canvas");
  const host = win.parent;
  const MARK = "mark[data-tt-id]";
  const STATUSES = ["open", "submitted", "resolved"];
  const MAX_ITEMS = 500;
  const QUESTIONS_ID = "tt-questions";
  const ANSWER_DEBOUNCE_MS = 1000;
  const QUESTION_STATUSES = ["open", "answered", "withdrawn"];
  const LABEL_KEYS = [
    "heading",
    "kindProduct",
    "kindTechnical",
    "blocking",
    "recommendedPrefix",
    "answerLabel",
    "answerPlaceholder",
    "unanswered",
    "recommendedStands",
  ];

  interface Item {
    id: string;
    quote: string;
    prefix: string;
    suffix: string;
    status: string;
    active: boolean;
  }

  interface QuestionLabels {
    heading: string;
    kindProduct: string;
    kindTechnical: string;
    blocking: string;
    recommendedPrefix: string;
    answerLabel: string;
    answerPlaceholder: string;
    unanswered: string;
    recommendedStands: string;
  }

  interface QuestionItem {
    id: string;
    key: string;
    kind: string;
    blocking: boolean;
    prompt: string;
    recommendedAnswer: string;
    answer: string;
    status: string;
    editable: boolean;
  }

  let items: Item[] = [];
  let lastSelection = "";
  let questionItems: QuestionItem[] = [];
  let questionLabels: QuestionLabels | null = null;
  const answerTimers = new Map<string, number>();

  const send = (message: Record<string, unknown>) => {
    host.postMessage(message, "*");
  };

  const isItem = (value: unknown): value is Item => {
    if (!value || typeof value !== "object") return false;
    const v = value as Record<string, unknown>;
    return (
      typeof v.id === "string" &&
      v.id.length > 0 &&
      v.id.length <= 200 &&
      typeof v.quote === "string" &&
      typeof v.prefix === "string" &&
      typeof v.suffix === "string" &&
      typeof v.status === "string" &&
      STATUSES.indexOf(v.status) >= 0
    );
  };

  const isQuestionItem = (value: unknown): value is QuestionItem => {
    if (!value || typeof value !== "object") return false;
    const v = value as Record<string, unknown>;
    return (
      typeof v.id === "string" &&
      v.id.length > 0 &&
      v.id.length <= 200 &&
      typeof v.key === "string" &&
      (v.kind === "product" || v.kind === "technical") &&
      typeof v.blocking === "boolean" &&
      typeof v.prompt === "string" &&
      typeof v.recommendedAnswer === "string" &&
      typeof v.answer === "string" &&
      typeof v.status === "string" &&
      QUESTION_STATUSES.indexOf(v.status) >= 0 &&
      typeof v.editable === "boolean"
    );
  };

  const isQuestionLabels = (value: unknown): value is QuestionLabels => {
    if (!value || typeof value !== "object") return false;
    const v = value as Record<string, unknown>;
    for (let i = 0; i < LABEL_KEYS.length; i++) {
      if (typeof v[LABEL_KEYS[i]] !== "string") return false;
    }
    return true;
  };

  const sendAnswer = (id: string, text: string) => {
    const timer = answerTimers.get(id);
    if (timer !== undefined) {
      win.clearTimeout(timer);
      answerTimers.delete(id);
    }
    send({ type: "tt:answer", id, text });
  };

  const scheduleAnswer = (id: string, text: string) => {
    const timer = answerTimers.get(id);
    if (timer !== undefined) win.clearTimeout(timer);
    answerTimers.set(
      id,
      win.setTimeout(() => {
        answerTimers.delete(id);
        sendAnswer(id, text);
      }, ANSWER_DEBOUNCE_MS),
    );
  };

  const findChild = (parent: Element, attr: string, value: string): Element | null => {
    for (let i = 0; i < parent.children.length; i++) {
      if (parent.children[i].getAttribute(attr) === value) return parent.children[i];
    }
    return null;
  };

  // Rebuilds each question's static parts every time (cheap, never focusable)
  // but never recreates — and only conditionally overwrites the value of — the
  // one `<textarea>` the reader may be mid-sentence in, so a poll landing
  // while they type neither steals focus nor clobbers what they are writing.
  const updateQuestionArticle = (article: Element, item: QuestionItem, labels: QuestionLabels) => {
    article.setAttribute("data-tt-status", item.status);

    let head = article.querySelector(".tt-q-head");
    if (!head) {
      head = doc.createElement("div");
      head.className = "tt-q-head";
      article.appendChild(head);
    }
    head.textContent = "";
    const key = doc.createElement("span");
    key.className = "tt-q-key";
    key.textContent = item.key;
    head.appendChild(key);
    const kindBadge = doc.createElement("span");
    kindBadge.className = "tt-q-badge";
    kindBadge.textContent = item.kind === "product" ? labels.kindProduct : labels.kindTechnical;
    head.appendChild(kindBadge);
    if (item.blocking) {
      const blockingBadge = doc.createElement("span");
      blockingBadge.className = "tt-q-badge tt-q-badge--blocking";
      blockingBadge.textContent = labels.blocking;
      head.appendChild(blockingBadge);
    }

    let promptEl = article.querySelector(".tt-q-prompt");
    if (!promptEl) {
      promptEl = doc.createElement("p");
      promptEl.className = "tt-q-prompt";
      article.appendChild(promptEl);
    }
    promptEl.textContent = item.prompt;

    let recommended = article.querySelector(".tt-q-recommended");
    if (!item.blocking && item.recommendedAnswer) {
      if (!recommended) {
        recommended = doc.createElement("p");
        recommended.className = "tt-q-recommended";
        article.appendChild(recommended);
      }
      recommended.textContent = labels.recommendedPrefix + item.recommendedAnswer;
    } else if (recommended) {
      recommended.remove();
    }

    let wrap = article.querySelector(".tt-q-answer-wrap");
    if (!wrap) {
      wrap = doc.createElement("div");
      wrap.className = "tt-q-answer-wrap";
      article.appendChild(wrap);
    }

    if (item.editable) {
      const stalePara = wrap.querySelector("p");
      if (stalePara) stalePara.remove();
      const textareaId = "tt-q-answer-" + item.id;
      let label = wrap.querySelector("label");
      if (!label) {
        label = doc.createElement("label");
        label.className = "tt-q-answer-label";
        wrap.appendChild(label);
      }
      label.setAttribute("for", textareaId);
      label.textContent = labels.answerLabel;

      let textarea = wrap.querySelector("textarea") as HTMLTextAreaElement | null;
      if (!textarea) {
        textarea = doc.createElement("textarea");
        textarea.className = "tt-q-answer";
        textarea.id = textareaId;
        textarea.setAttribute("data-tt-answer-id", item.id);
        textarea.addEventListener("input", () => scheduleAnswer(item.id, (textarea as HTMLTextAreaElement).value));
        textarea.addEventListener("blur", () => sendAnswer(item.id, (textarea as HTMLTextAreaElement).value));
        wrap.appendChild(textarea);
      }
      textarea.placeholder = labels.answerPlaceholder;
      if (doc.activeElement !== textarea) textarea.value = item.answer;
    } else {
      const label = wrap.querySelector("label");
      if (label) label.remove();
      const textarea = wrap.querySelector("textarea");
      if (textarea) textarea.remove();
      let para = wrap.querySelector("p");
      if (!para) {
        para = doc.createElement("p");
        wrap.appendChild(para);
      }
      const text = item.answer || (item.blocking ? labels.unanswered : labels.recommendedStands);
      para.className = "tt-q-answer-static" + (item.answer ? "" : " tt-q-answer-static--empty");
      para.textContent = text;
    }
  };

  const renderQuestions = () => {
    const root = doc.getElementById(QUESTIONS_ID);
    if (!root) return;
    if (!questionLabels || questionItems.length === 0) {
      root.hidden = true;
      root.textContent = "";
      return;
    }
    root.hidden = false;
    let heading = root.querySelector(".tt-q-heading");
    if (!heading) {
      heading = doc.createElement("h2");
      heading.className = "tt-q-heading";
      root.insertBefore(heading, root.firstChild);
    }
    heading.textContent = questionLabels.heading;

    const seen = new Set<string>();
    for (const item of questionItems) {
      seen.add(item.id);
      let article = findChild(root, "data-tt-question-id", item.id);
      if (!article) {
        article = doc.createElement("article");
        article.className = "tt-q";
        article.setAttribute("data-tt-question-id", item.id);
        root.appendChild(article);
      }
      updateQuestionArticle(article, item, questionLabels);
    }
    for (let i = root.children.length - 1; i >= 0; i--) {
      const child = root.children[i];
      const id = child.getAttribute("data-tt-question-id");
      if (id && !seen.has(id)) child.remove();
    }
  };

  const paint = () => {
    const body = doc.body;
    if (!body) return;
    kit.clear(body, MARK);
    const index = kit.collect(body);
    const results: { id: string; found: boolean }[] = [];
    const spans: { span: TextSpan; make: () => Element }[] = [];
    for (const item of items) {
      const span = kit.locate(index, item);
      results.push({ id: item.id, found: span !== null });
      if (!span) continue;
      spans.push({
        span,
        make: () => {
          const mark = doc.createElement("mark");
          mark.setAttribute("data-tt-id", item.id);
          mark.setAttribute("data-tt-status", item.status);
          if (item.active) mark.setAttribute("data-tt-active", "true");
          return mark;
        },
      });
    }
    kit.wrapAll(body, spans);
    send({ type: "tt:anchored", results });
  };

  const reportSelection = () => {
    const selection = win.getSelection();
    if (!selection || selection.rangeCount === 0 || selection.isCollapsed) {
      lastSelection = "";
      return;
    }
    const range = selection.getRangeAt(0);
    const body = doc.body;
    if (!body || !body.contains(range.commonAncestorContainer)) return;
    const selector = kit.describe(body, range);
    if (!selector) return;
    const key = selector.prefix + "\u0000" + selector.quote + "\u0000" + selector.suffix;
    if (key === lastSelection) return;
    lastSelection = key;
    const box = typeof range.getBoundingClientRect === "function" ? range.getBoundingClientRect() : null;
    send({
      type: "tt:selection",
      quote: selector.quote,
      prefix: selector.prefix,
      suffix: selector.suffix,
      rect: box
        ? { top: box.top, left: box.left, width: box.width, height: box.height }
        : { top: 0, left: 0, width: 0, height: 0 },
    });
  };

  const elementOf = (target: EventTarget | null): Element | null => {
    const node = target as Node | null;
    if (!node) return null;
    return node.nodeType === 1 ? (node as Element) : node.parentElement;
  };

  const findMark = (id: string): Element | null => {
    const marks = doc.querySelectorAll(MARK);
    for (let i = 0; i < marks.length; i++) {
      if (marks[i].getAttribute("data-tt-id") === id) return marks[i];
    }
    return null;
  };

  doc.addEventListener("mousedown", () => {
    lastSelection = "";
  });
  doc.addEventListener("mouseup", () => {
    win.setTimeout(reportSelection, 0);
  });
  doc.addEventListener("keyup", reportSelection);

  // A sandboxed frame may still navigate itself, so an ordinary link would
  // replace the document with whatever page it points at. Only in-document
  // anchors do anything.
  doc.addEventListener("click", (event) => {
    const element = elementOf(event.target);
    if (!element) return;
    const link = element.closest("a[href]");
    if (link) {
      event.preventDefault();
      const href = link.getAttribute("href") || "";
      if (href.charAt(0) === "#" && href.length > 1) {
        let destination: Element | null = null;
        try {
          destination = doc.getElementById(decodeURIComponent(href.slice(1)));
        } catch {
          destination = null;
        }
        if (!canvas && destination && typeof destination.scrollIntoView === "function") {
          destination.scrollIntoView({ block: "start" });
        }
      }
    }
    const selection = win.getSelection();
    if (selection && !selection.isCollapsed) return;
    const mark = element.closest(MARK);
    const id = mark ? mark.getAttribute("data-tt-id") : null;
    if (id) send({ type: "tt:focus", id });
  });

  win.addEventListener("message", (event) => {
    if (event.source !== host) return;
    const data = event.data;
    if (!data || typeof data !== "object") return;
    if (data.type === "tt:annotations" && Array.isArray(data.items)) {
      items = data.items.filter(isItem).slice(0, MAX_ITEMS);
      paint();
    } else if (data.type === "tt:questions" && Array.isArray(data.items)) {
      if (isQuestionLabels(data.labels)) questionLabels = data.labels;
      questionItems = data.items.filter(isQuestionItem).slice(0, MAX_ITEMS);
      renderQuestions();
    } else if (data.type === "tt:scrollTo" && typeof data.id === "string") {
      const mark = findMark(data.id);
      if (!canvas && mark && typeof mark.scrollIntoView === "function") {
        mark.scrollIntoView({ block: "center", behavior: "smooth" });
      }
    }
  });

  send({ type: "tt:ready" });
}

export function buildFrameScript(): string {
  return (
    `(${frameRuntime.toString()})((${textQuoteKit.toString()})(), window);` +
    `(${canvasRuntime.toString()})(window);`
  );
}
