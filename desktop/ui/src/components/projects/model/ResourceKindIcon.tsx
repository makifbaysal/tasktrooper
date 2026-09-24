import {
  Activity,
  Box,
  CreditCard,
  Database,
  Globe,
  HardDrive,
  KeyRound,
  ListOrdered,
  Mail,
  Search,
  Sparkles,
  Zap,
  type LucideIcon,
} from "lucide-react";
import type { ResourceKind } from "@/api";
import { cn } from "@/lib/utils";

const RESOURCE_ICONS: Record<ResourceKind, LucideIcon> = {
  database: Database,
  cache: Zap,
  queue: ListOrdered,
  storage: HardDrive,
  search: Search,
  api: Globe,
  auth: KeyRound,
  email: Mail,
  payments: CreditCard,
  ai: Sparkles,
  observability: Activity,
  other: Box,
};

interface ResourceKindIconProps {
  kind: ResourceKind;
  className?: string;
}

export function ResourceKindIcon({ kind, className }: ResourceKindIconProps) {
  const Icon = RESOURCE_ICONS[kind] ?? Box;
  return <Icon className={cn("h-4 w-4", className)} aria-hidden />;
}
