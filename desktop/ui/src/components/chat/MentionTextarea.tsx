import { forwardRef, useImperativeHandle, useMemo, useRef, useState, type TextareaHTMLAttributes } from "react";
import { MentionMenu, type MentionOption } from "@/components/chat/MentionMenu";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

const MENTION_QUERY_MAX = 40;
const MENTION_RESULTS_MAX = 8;
const KIND_ORDER: Record<MentionOption["kind"], number> = { agent: 0, project: 1, repository: 2 };

function normalize(value: string) {
  return value.toLocaleLowerCase("tr");
}

// Active @token at the caret: the nearest '@' on the same line that sits at a
// word boundary. Returns null when the caret is not inside a mention.
function findMentionToken(value: string, caret: number): { start: number; query: string } | null {
  for (let i = caret - 1; i >= 0 && caret - i <= MENTION_QUERY_MAX + 1; i--) {
    const ch = value[i];
    if (ch === "\n") return null;
    if (ch === "@") {
      const before = i === 0 ? "" : value[i - 1];
      if (before && /[\p{L}\p{N}]/u.test(before)) return null;
      return { start: i, query: value.slice(i + 1, caret) };
    }
  }
  return null;
}

type NativeProps = Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange" | "onKeyDown" | "onClick" | "onBlur">;

interface MentionTextareaProps extends NativeProps {
  value: string;
  onChange: (value: string) => void;
  /** Enter without Shift, when the @-menu is not taking the key. */
  onSubmit: () => void;
  mentionOptions?: MentionOption[];
  // Fired when the user picks an entity from the autocomplete, so the parent
  // can send the exact kind+id reference alongside the message text.
  onMentionSelect?: (option: MentionOption) => void;
  /** Classes for the positioning wrapper the @-menu anchors to. */
  wrapperClassName?: string;
}

export const MentionTextarea = forwardRef<HTMLTextAreaElement, MentionTextareaProps>(function MentionTextarea(
  { value, onChange, onSubmit, mentionOptions = [], onMentionSelect, wrapperClassName, ...textareaProps },
  ref,
) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  useImperativeHandle(ref, () => textareaRef.current as HTMLTextAreaElement);
  const [mention, setMention] = useState<{ start: number; query: string } | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const filtered = useMemo(() => {
    if (!mention || mentionOptions.length === 0) return [];
    const q = normalize(mention.query.trim());
    return mentionOptions
      .filter((o) => normalize(o.name).includes(q) || (o.description ? normalize(o.description).includes(q) : false))
      .sort((a, b) => KIND_ORDER[a.kind] - KIND_ORDER[b.kind] || a.name.localeCompare(b.name, "tr"))
      .slice(0, MENTION_RESULTS_MAX);
  }, [mention, mentionOptions]);

  const menuOpen = mention !== null && filtered.length > 0;

  const syncMention = (nextValue: string, caret: number) => {
    setMention(findMentionToken(nextValue, caret));
    setActiveIndex(0);
  };

  const applyMention = (option: MentionOption) => {
    if (!mention) return;
    const caret = textareaRef.current?.selectionStart ?? value.length;
    const inserted = `@${option.name} `;
    onChange(value.slice(0, mention.start) + inserted + value.slice(caret));
    onMentionSelect?.(option);
    setMention(null);
    const cursor = mention.start + inserted.length;
    requestAnimationFrame(() => {
      textareaRef.current?.focus();
      textareaRef.current?.setSelectionRange(cursor, cursor);
    });
  };

  return (
    <div className={cn("relative", wrapperClassName)}>
      {menuOpen && (
        <MentionMenu options={filtered} activeIndex={activeIndex} onSelect={applyMention} onHover={setActiveIndex} />
      )}
      <Textarea
        {...textareaProps}
        ref={textareaRef}
        value={value}
        onChange={(e) => {
          onChange(e.target.value);
          syncMention(e.target.value, e.target.selectionStart ?? e.target.value.length);
        }}
        onKeyDown={(e) => {
          if (menuOpen) {
            if (e.key === "ArrowDown") {
              e.preventDefault();
              setActiveIndex((i) => (i + 1) % filtered.length);
              return;
            }
            if (e.key === "ArrowUp") {
              e.preventDefault();
              setActiveIndex((i) => (i - 1 + filtered.length) % filtered.length);
              return;
            }
            if (e.key === "Enter" || e.key === "Tab") {
              e.preventDefault();
              applyMention(filtered[activeIndex] ?? filtered[0]);
              return;
            }
            if (e.key === "Escape") {
              e.preventDefault();
              setMention(null);
              return;
            }
          }
          if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            onSubmit();
          }
        }}
        onClick={(e) => syncMention(value, e.currentTarget.selectionStart ?? value.length)}
        onBlur={() => setMention(null)}
      />
    </div>
  );
});
