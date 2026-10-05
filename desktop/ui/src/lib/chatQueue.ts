import type { AttachmentMeta, MessageMention } from "@/api";

export interface QueuedMessage {
  id: string;
  content: string;
  mentions: MessageMention[];
  attachments: AttachmentMeta[];
  fileIds: string[];
  createdAt: string;
}

export interface MergedQueue {
  content: string;
  mentions: MessageMention[];
  attachments: AttachmentMeta[];
  fileIds: string[];
}

const storageKey = (sessionId: string) => `tt.chat.queue.${sessionId}`;

export function mergeQueued(items: QueuedMessage[]): MergedQueue {
  const mentions: MessageMention[] = [];
  const attachments: AttachmentMeta[] = [];
  const fileIds: string[] = [];
  for (const item of items) {
    for (const m of item.mentions) {
      if (!mentions.some((x) => x.kind === m.kind && x.id === m.id)) mentions.push(m);
    }
    for (const a of item.attachments) {
      if (!attachments.some((x) => x.id === a.id)) attachments.push(a);
    }
    for (const id of item.fileIds) {
      if (!fileIds.includes(id)) fileIds.push(id);
    }
  }
  return { content: items.map((i) => i.content).join("\n\n"), mentions, attachments, fileIds };
}

export function loadQueue(sessionId: string): QueuedMessage[] {
  try {
    const raw = localStorage.getItem(storageKey(sessionId));
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter(
      (item): item is QueuedMessage =>
        !!item &&
        typeof item.id === "string" &&
        typeof item.content === "string" &&
        Array.isArray(item.mentions) &&
        Array.isArray(item.attachments) &&
        Array.isArray(item.fileIds),
    );
  } catch {
    return [];
  }
}

export function saveQueue(sessionId: string, items: QueuedMessage[]): void {
  try {
    if (items.length === 0) localStorage.removeItem(storageKey(sessionId));
    else localStorage.setItem(storageKey(sessionId), JSON.stringify(items));
  } catch {
    /* storage unavailable: the queue just stops surviving a reload */
  }
}
