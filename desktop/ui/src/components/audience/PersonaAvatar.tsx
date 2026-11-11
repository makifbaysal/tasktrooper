import { initials } from "@/lib/audienceHub";
import { cn } from "@/lib/utils";

/** A person without a photo: their initials on a calm disc. The people are invented, so there is no face to show. */
export function PersonaAvatar({ name, className }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn("flex h-9 w-9 shrink-0 items-center justify-center rounded-full bg-primary/10 text-caption font-semibold text-primary", className)}
    >
      {initials(name)}
    </span>
  );
}
