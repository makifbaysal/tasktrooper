/**
 * The tail of an installer's output, as the page shows it: the last 40 lines,
 * each at most 300 characters, with terminal colour codes and cursor movement
 * taken out — installers write for a terminal, and the page is not one.
 */

export const LOG_LINES = 40;
export const LOG_LINE_CHARS = 300;

// eslint-disable-next-line no-control-regex -- matching escape sequences is the entire point.
const ANSI = /\u001b\[[0-?]*[ -/]*[@-~]|\u001b\][^\u0007\u001b]*(?:\u0007|\u001b\\)|\u001b[@-Z\\-_]/g;
// eslint-disable-next-line no-control-regex -- what is left after the sequences: bells, backspaces, stray controls.
const CONTROL = /[\u0000-\u0008\u000b-\u001f\u007f]/g;

export function cleanLine(raw: string): string {
  const visible = raw.replace(ANSI, "").replace(CONTROL, "").replace(/\s+$/, "");
  return visible.length > LOG_LINE_CHARS ? `${visible.slice(0, LOG_LINE_CHARS - 1)}…` : visible;
}

export class LogRing {
  #lines: string[] = [];

  constructor(initial: readonly string[] = []) {
    for (const line of initial) this.push(line);
  }

  push(raw: string): void {
    const line = cleanLine(raw);
    if (line.trim() === "") return;
    this.#lines.push(line);
    if (this.#lines.length > LOG_LINES) this.#lines.splice(0, this.#lines.length - LOG_LINES);
  }

  get lines(): string[] {
    return [...this.#lines];
  }

  get size(): number {
    return this.#lines.length;
  }
}
