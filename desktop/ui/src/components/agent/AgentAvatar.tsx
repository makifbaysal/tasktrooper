import { agentInitials } from "@/lib/leadAgent";
import { cn } from "@/lib/utils";

export type AgentAvatarSize = "xs" | "sm" | "md" | "lg" | "xl";

interface AgentAvatarProps {
  name: string;
  /** The lead agent: navy tile with warm initials and a warm ring, so it never reads as one of the team. */
  lead?: boolean;
  size?: AgentAvatarSize;
  className?: string;
}

const sizeClasses: Record<AgentAvatarSize, string> = {
  xs: "h-5 w-5 text-[9px]",
  sm: "h-6 w-6 text-[10px]",
  md: "h-8 w-8 text-[11px]",
  lg: "h-11 w-11 text-sm",
  xl: "h-14 w-14 text-lg",
};

const leadRadius: Record<AgentAvatarSize, string> = {
  xs: "rounded-[5px]",
  sm: "rounded-md",
  md: "rounded-lg",
  lg: "rounded-xl",
  xl: "rounded-2xl",
};

export function AgentAvatar({ name, lead = false, size = "sm", className }: AgentAvatarProps) {
  return (
    <span
      aria-hidden
      className={cn(
        "flex shrink-0 select-none items-center justify-center font-semibold tracking-wide",
        sizeClasses[size],
        lead
          ? cn(
              "bg-brand-mark font-bold text-accent-warm ring-[1.5px] ring-accent-warm ring-offset-2 ring-offset-background",
              leadRadius[size],
            )
          : "rounded-full bg-muted text-muted-foreground",
        className,
      )}
    >
      {agentInitials(name)}
    </span>
  );
}
