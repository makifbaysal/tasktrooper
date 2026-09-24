import { Cloud, Server, Triangle, type LucideIcon } from "lucide-react";
import type { CloudProviderKind } from "@/api";
import { cn } from "@/lib/utils";

const PROVIDER_ICONS: Record<CloudProviderKind, LucideIcon> = {
  vercel: Triangle,
  gcp: Cloud,
  aws: Server,
};

interface ProviderIconProps {
  provider: CloudProviderKind;
  className?: string;
}

/** A cloud provider's mark: a plain lucide glyph on theme tokens, never a
 * brand logo image. */
export function ProviderIcon({ provider, className }: ProviderIconProps) {
  const Icon = PROVIDER_ICONS[provider] ?? Cloud;
  return <Icon className={cn("h-4 w-4", className)} aria-hidden />;
}
