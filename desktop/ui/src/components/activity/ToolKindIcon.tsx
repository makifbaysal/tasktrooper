import {
  Bot,
  FileSearch,
  FileText,
  Globe,
  GitBranch,
  ListChecks,
  Monitor,
  Pencil,
  FilePlus,
  SquareKanban,
  Terminal,
  Wrench,
  type LucideIcon,
} from "lucide-react";
import type { ToolKind } from "@/lib/activityFeed";
import { cn } from "@/lib/utils";

const ICONS: Record<ToolKind, LucideIcon> = {
  read: FileText,
  search: FileSearch,
  edit: Pencil,
  write: FilePlus,
  shell: Terminal,
  browser: Monitor,
  web: Globe,
  board: SquareKanban,
  git: GitBranch,
  plan: ListChecks,
  subagent: Bot,
  other: Wrench,
};

export function ToolKindIcon({ kind, className }: { kind: ToolKind; className?: string }) {
  const Icon = ICONS[kind];
  return <Icon aria-hidden className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground", className)} />;
}
