import { AlertTriangle, Bot, Check, ChevronDown, ChevronRight, GitBranch, Info, X } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import type { OrchestrationPlan } from "@/api";
import { AttachmentImage } from "@/components/attachments/AttachmentImage";
import { PlanView } from "@/components/chat/PlanView";
import { MarkdownContent } from "@/components/markdown/MarkdownContent";
import { badgeVariants } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Separator } from "@/components/ui/separator";
import { useI18n } from "@/hooks/useI18n";
import {
  INTERRUPTED_ERROR,
  type FeedItem,
  type FeedLane,
  type FeedTone,
  type FeedTool,
} from "@/lib/activityFeed";
import {
  currentLabel,
  durationBetween,
  eventLabel,
  formatClock,
  formatToolDuration,
  laneTag,
  laneTitle,
  prettyJson,
  toolGroupLabel,
  toolLabel,
} from "@/lib/activityFeedLabels";
import { cn } from "@/lib/utils";
import { FeedStatusIcon } from "./FeedStatusIcon";
import { feedPre, feedRowButton } from "./feedStyles";
import { ToolKindIcon } from "./ToolKindIcon";

export interface FeedFocus {
  id: string;
  nonce: number;
}

export interface FeedContext {
  plan: OrchestrationPlan | null;
  agentNameMap?: Record<string, string>;
  focus: FeedFocus | null;
}

type ToolsItem = Extract<FeedItem, { kind: "tools" }>;
type EventItem = Extract<FeedItem, { kind: "event" }>;
type UserItem = Extract<FeedItem, { kind: "user" }>;

function Chevron({ open }: { open: boolean }) {
  const Icon = open ? ChevronDown : ChevronRight;
  return <Icon aria-hidden className="shrink-0 text-muted-foreground" />;
}

function laneContains(lane: FeedLane, id: string): boolean {
  return lane.items.some((item) => item.kind === "lane" && (item.lane.id === id || laneContains(item.lane, id)));
}

function isLong(text: string, chars: number, lines: number): boolean {
  return text.length > chars || text.split("\n").length > lines;
}

function Highlighted({ text, target }: { text: string; target?: string }) {
  const at = target ? text.indexOf(target) : -1;
  if (!target || at < 0) return <>{text}</>;
  return (
    <>
      {text.slice(0, at)}
      <span className="font-medium text-foreground">{target}</span>
      {text.slice(at + target.length)}
    </>
  );
}

function ClampedText({ text, className }: { text: string; className?: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const long = isLong(text, 140, 2);
  return (
    <div className="min-w-0">
      <p className={cn("whitespace-pre-wrap text-muted-foreground [overflow-wrap:anywhere]", !open && "line-clamp-2", className)}>
        {text}
      </p>
      {long && (
        <Button
          type="button"
          variant="link"
          className="h-auto p-0 text-micro"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? t("activityArea.feed.less") : t("activityArea.feed.more")}
        </Button>
      )}
    </div>
  );
}

function Label({ children }: { children: ReactNode }) {
  return <p className="mb-1 text-micro font-medium uppercase tracking-wide text-muted-foreground">{children}</p>;
}

function ToolDetail({ tool }: { tool: FeedTool }) {
  const { t } = useI18n();
  const args = prettyJson(tool.arguments);
  const hasArgs = args !== "" && args !== "{}";
  return (
    <div className="min-w-0 space-y-2 px-2 pb-2 pt-1">
      {hasArgs && (
        <div>
          <Label>{t("activityArea.feed.input")}</Label>
          <pre className={feedPre}>{args}</pre>
        </div>
      )}
      <div>
        <Label>{t("activityArea.feed.output")}</Label>
        {tool.result ? (
          <pre className={cn(feedPre, tool.isError && "bg-destructive/10 text-destructive")}>{tool.result}</pre>
        ) : (
          <p className="text-caption text-muted-foreground">{t("activityArea.feed.noOutput")}</p>
        )}
      </div>
      {tool.imageIds && tool.imageIds.length > 0 && (
        <div className="grid gap-2 sm:grid-cols-2">
          {tool.imageIds.map((id) => (
            <AttachmentImage key={id} id={id} alt={toolLabel(t, tool)} />
          ))}
        </div>
      )}
    </div>
  );
}

function ToolRow({ tool }: { tool: FeedTool }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const label = toolLabel(t, tool);
  const duration = durationBetween(tool.startedAt, tool.endedAt);
  return (
    <li className="min-w-0">
      <Button
        type="button"
        variant="ghost"
        className={feedRowButton}
        aria-expanded={open}
        aria-label={label}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="min-w-0 flex-1 truncate" title={tool.targetDetail ?? label}>
          <Highlighted text={label} target={tool.target} />
        </span>
        {duration !== null && (
          <span className="shrink-0 tabular-nums text-micro text-muted-foreground">{formatToolDuration(duration)}</span>
        )}
        <FeedStatusIcon status={tool.status} />
        <Chevron open={open} />
      </Button>
      {open && <ToolDetail tool={tool} />}
    </li>
  );
}

function previewTargets(item: ToolsItem): string | null {
  const names = [...new Set(item.tools.map((tool) => tool.target).filter((x): x is string => !!x))];
  if (names.length === 0) return null;
  return names.slice(0, 3).join(", ") + (names.length > 3 ? "…" : "");
}

function ToolGroup({ item }: { item: ToolsItem }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const single = item.tools.length === 1;
  const label = toolGroupLabel(t, item);
  const last = item.tools[item.tools.length - 1];
  const duration = item.status === "running" ? null : durationBetween(item.tools[0].startedAt, last.endedAt);
  const preview = single ? (item.tools[0].targetDetail !== item.tools[0].target ? item.tools[0].targetDetail : null) : previewTargets(item);
  return (
    <div className="min-w-0">
      <Button
        type="button"
        variant="ghost"
        className={cn(feedRowButton, "items-start")}
        aria-expanded={open}
        aria-label={label}
        onClick={() => setOpen((v) => !v)}
      >
        <ToolKindIcon kind={item.toolKind} className="mt-0.5" />
        <span className="min-w-0 flex-1">
          <span className="block truncate" title={label}>
            <Highlighted text={label} target={single ? item.tools[0].target : undefined} />
          </span>
          {preview && <span className="block truncate text-micro text-muted-foreground">{preview}</span>}
        </span>
        {duration !== null && (
          <span className="mt-0.5 shrink-0 tabular-nums text-micro text-muted-foreground">{formatToolDuration(duration)}</span>
        )}
        <span className="mt-0.5 flex shrink-0 items-center gap-1.5">
          <FeedStatusIcon status={item.status} />
          <Chevron open={open} />
        </span>
      </Button>
      {open && (single ? <ToolDetail tool={item.tools[0]} /> : (
        <ul className="ml-3 space-y-0.5 border-l border-border pl-2">
          {item.tools.map((tool) => (
            <ToolRow key={tool.id} tool={tool} />
          ))}
        </ul>
      ))}
    </div>
  );
}

function SayBlock({ content }: { content: string }) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  const long = isLong(content, 280, 5);
  return (
    <div className="min-w-0">
      <div
        className={cn(
          "min-w-0 text-body [overflow-wrap:anywhere]",
          long && !open && "max-h-24 overflow-hidden [mask-image:linear-gradient(to_bottom,black_55%,transparent)]",
        )}
      >
        <MarkdownContent content={content} />
      </div>
      {long && (
        <Button
          type="button"
          variant="link"
          className="h-auto p-0 text-caption"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          {open ? t("activityArea.feed.less") : t("activityArea.feed.more")}
        </Button>
      )}
    </div>
  );
}

function DividerLabel({ children }: { children: ReactNode }) {
  return (
    <div className="flex items-center gap-2 text-micro font-medium text-muted-foreground">
      <span className="shrink-0">{children}</span>
      <Separator className="flex-1" />
    </div>
  );
}

function UserBlock({ item }: { item: UserItem }) {
  const { t, lang } = useI18n();
  const [open, setOpen] = useState(false);
  const long = isLong(item.content, 160, 3);
  const clock = formatClock(item.at, lang);
  const body = "rounded-lg bg-muted px-3 py-2 text-body [overflow-wrap:anywhere]";
  return (
    <div className="min-w-0 space-y-1.5">
      <DividerLabel>
        {t("activityArea.feed.you")}
        {clock && ` · ${clock}`}
      </DividerLabel>
      {long ? (
        <Button
          type="button"
          variant="ghost"
          className={cn(body, "h-auto w-full justify-start whitespace-pre-wrap text-left font-normal hover:bg-muted")}
          aria-expanded={open}
          aria-label={item.content.slice(0, 80)}
          onClick={() => setOpen((v) => !v)}
        >
          <span className={cn("min-w-0 flex-1", !open && "line-clamp-3")}>{item.content}</span>
        </Button>
      ) : (
        <div className={cn(body, "whitespace-pre-wrap")}>{item.content}</div>
      )}
    </div>
  );
}

function ToneIcon({ tone, status }: { tone: FeedTone; status: EventItem["status"] }) {
  const cls = "mt-0.5 h-3.5 w-3.5 shrink-0";
  if (status === "running") return <FeedStatusIcon status="running" className="mt-0.5" />;
  if (tone === "success") return <Check aria-hidden className={cn(cls, "text-success")} />;
  if (tone === "danger") return <X aria-hidden className={cn(cls, "text-destructive")} />;
  if (tone === "warning") return <AlertTriangle aria-hidden className={cn(cls, "text-warning")} />;
  if (tone === "info") return <Info aria-hidden className={cn(cls, "text-info")} />;
  return <span aria-hidden className="mt-1.5 flex h-3.5 w-3.5 shrink-0 items-center justify-center"><span className="h-1.5 w-1.5 rounded-full bg-muted-foreground/50" /></span>;
}

function EventRow({ item, ctx }: { item: EventItem; ctx: FeedContext }) {
  const { t } = useI18n();
  const [showPlan, setShowPlan] = useState(false);
  const plan = ctx.plan && item.planId && ctx.plan.id === item.planId ? ctx.plan : null;
  return (
    <div className="flex min-w-0 items-start gap-2 text-caption">
      <ToneIcon tone={item.tone} status={item.status} />
      <div className="min-w-0 flex-1 space-y-0.5">
        <p className={cn("[overflow-wrap:anywhere]", item.tone === "muted" ? "text-muted-foreground" : "font-medium")}>
          {eventLabel(t, item)}
        </p>
        {item.detail && <ClampedText text={item.detail} />}
        {plan && (
          <Button
            type="button"
            variant="link"
            className="h-auto p-0 text-caption"
            aria-expanded={showPlan}
            onClick={() => setShowPlan((v) => !v)}
          >
            {showPlan ? t("activityArea.feed.hidePlan") : t("activityArea.feed.showPlan")}
          </Button>
        )}
        {plan && showPlan && (
          <Card className="mt-2 min-w-0 p-3 shadow-none">
            <PlanView plan={plan} agentNameMap={ctx.agentNameMap} />
          </Card>
        )}
      </div>
    </div>
  );
}

function LaneBlock({ lane, ctx }: { lane: FeedLane; ctx: FeedContext }) {
  const { t } = useI18n();
  const ref = useRef<HTMLDivElement>(null);
  const [userOpen, setUserOpen] = useState<boolean | null>(null);
  const open = userOpen ?? (lane.status === "running" || lane.status === "failed");
  const [resultOpen, setResultOpen] = useState(false);

  // The feed is rebuilt on every poll, so `lane` is a new object each time; a
  // jump must be acted on once, not again on every tick after it.
  const focus = ctx.focus;
  const handledFocus = useRef<FeedFocus | null>(null);
  useEffect(() => {
    if (!focus || handledFocus.current === focus) return;
    handledFocus.current = focus;
    if (focus.id === lane.id) {
      setUserOpen(true);
      const frame = requestAnimationFrame(() => ref.current?.scrollIntoView?.({ block: "nearest" }));
      return () => cancelAnimationFrame(frame);
    }
    if (laneContains(lane, focus.id)) setUserOpen(true);
  }, [focus, lane]);

  const title = laneTitle(t, lane);
  const agent = lane.agentName ? (ctx.agentNameMap?.[lane.agentName] ?? lane.agentName) : null;
  const duration = durationBetween(lane.startedAt, lane.endedAt);
  const LaneIcon = lane.source === "subagent" ? Bot : GitBranch;
  const meta = [
    agent,
    lane.toolCount > 0 ? t("activityArea.lane.tools", { count: lane.toolCount }) : null,
    duration !== null ? formatToolDuration(duration) : null,
  ].filter(Boolean);

  const error = lane.error === INTERRUPTED_ERROR ? t("activityArea.lane.interrupted") : lane.error;

  return (
    <Card
      ref={ref}
      data-lane-id={lane.id}
      className="min-w-0 overflow-hidden border-l-2 border-l-info/60 bg-card/60 shadow-none"
    >
      <Button
        type="button"
        variant="ghost"
        className="h-auto w-full min-w-0 items-start justify-start gap-2 whitespace-normal rounded-none px-3 py-2 text-left font-normal [&_svg]:size-3.5"
        aria-expanded={open}
        aria-label={title}
        onClick={() => setUserOpen(!open)}
      >
        <LaneIcon aria-hidden className="mt-0.5 text-muted-foreground" />
        <span className="min-w-0 flex-1 space-y-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <span className={cn(badgeVariants({ variant: "secondary" }), "shrink-0 px-1.5 py-0 text-micro")}>
              {laneTag(t, lane)}
            </span>
            <span className="min-w-0 truncate text-caption font-medium" title={title}>
              {title}
            </span>
          </span>
          {meta.length > 0 && <span className="block truncate text-micro text-muted-foreground">{meta.join(" · ")}</span>}
          {lane.status === "running" && lane.current && (
            <span className="block truncate text-micro text-foreground/80">{currentLabel(t, lane.current)}</span>
          )}
        </span>
        <span className="mt-0.5 flex shrink-0 items-center gap-1.5">
          <FeedStatusIcon status={lane.status} />
          <Chevron open={open} />
        </span>
      </Button>
      {open && (
        <div className="min-w-0 space-y-2 border-t border-border px-3 py-2">
          {lane.items.length > 0 && <FeedItemList items={lane.items} ctx={ctx} nested />}
          {lane.result && (
            <div className="min-w-0">
              <Button
                type="button"
                variant="ghost"
                className={feedRowButton}
                aria-expanded={resultOpen}
                onClick={() => setResultOpen((v) => !v)}
              >
                <span className="min-w-0 flex-1 font-medium">{t("activityArea.lane.result")}</span>
                <Chevron open={resultOpen} />
              </Button>
              {resultOpen && (
                <div className="max-h-64 min-w-0 overflow-auto rounded-md bg-muted/50 p-2 text-body [overflow-wrap:anywhere]">
                  <MarkdownContent content={lane.result} />
                </div>
              )}
            </div>
          )}
          {lane.status === "failed" && error && (
            <Notice variant="error" title={error} className="rounded-lg px-3 py-2 text-caption [overflow-wrap:anywhere]" />
          )}
          {lane.status === "incomplete" && (
            <Notice variant="warning" title={t("activityArea.lane.incomplete")} className="rounded-lg px-3 py-2 text-caption [overflow-wrap:anywhere]">
              {lane.error}
            </Notice>
          )}
        </div>
      )}
    </Card>
  );
}

function markerClass(item: FeedItem): string {
  switch (item.kind) {
    case "say":
      return "bg-muted-foreground";
    case "lane":
      return "bg-info";
    case "event":
      return item.tone === "danger"
        ? "bg-destructive"
        : item.tone === "warning"
          ? "bg-warning"
          : item.tone === "success"
            ? "bg-success"
            : item.tone === "info"
              ? "bg-info"
              : "bg-border";
    default:
      return "bg-border";
  }
}

function ItemView({ item, ctx }: { item: FeedItem; ctx: FeedContext }) {
  switch (item.kind) {
    case "say":
      return <SayBlock content={item.content} />;
    case "tools":
      return <ToolGroup item={item} />;
    case "lane":
      return <LaneBlock lane={item.lane} ctx={ctx} />;
    case "event":
      return <EventRow item={item} ctx={ctx} />;
    default:
      return null;
  }
}

function RunDivider({ at }: { at: string }) {
  const { t, lang } = useI18n();
  const clock = formatClock(at, lang);
  return (
    <DividerLabel>
      {t("activityArea.feed.run")}
      {clock && ` · ${clock}`}
    </DividerLabel>
  );
}

type Segment = { kind: "boundary"; item: FeedItem } | { kind: "rail"; items: FeedItem[] };

function segment(items: FeedItem[]): Segment[] {
  const out: Segment[] = [];
  for (const item of items) {
    if (item.kind === "user" || item.kind === "run") {
      out.push({ kind: "boundary", item });
      continue;
    }
    const last = out[out.length - 1];
    if (last && last.kind === "rail") last.items.push(item);
    else out.push({ kind: "rail", items: [item] });
  }
  return out;
}

export function FeedItemList({ items, ctx, nested = false }: { items: FeedItem[]; ctx: FeedContext; nested?: boolean }) {
  if (nested) {
    return (
      <ul className="min-w-0 space-y-1.5">
        {items.map((item) => (
          <li key={item.id} className="min-w-0">
            <ItemView item={item} ctx={ctx} />
          </li>
        ))}
      </ul>
    );
  }
  return (
    <div className="min-w-0 space-y-3">
      {segment(items).map((seg, index) =>
        seg.kind === "boundary" ? (
          seg.item.kind === "user" ? (
            <UserBlock key={seg.item.id} item={seg.item} />
          ) : seg.item.kind === "run" ? (
            <RunDivider key={seg.item.id} at={seg.item.at} />
          ) : null
        ) : (
          <ol key={`rail-${seg.items[0].id}-${index}`} className="ml-2 min-w-0 space-y-2.5 border-l border-border pl-4">
            {seg.items.map((item) => (
              <li key={item.id} className="relative min-w-0">
                <span
                  aria-hidden
                  className={cn("absolute -left-[20px] top-2 h-1.5 w-1.5 rounded-full ring-2 ring-background", markerClass(item))}
                />
                <ItemView item={item} ctx={ctx} />
              </li>
            ))}
          </ol>
        ),
      )}
    </div>
  );
}
