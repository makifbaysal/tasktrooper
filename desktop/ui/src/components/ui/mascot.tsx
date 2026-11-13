import { cn } from "@/lib/utils";

// Trooper, the TaskTrooper mascot. Each pose is one state the app can be in;
// pick the pose by what the screen is telling the person, not by looks:
//   wave       welcome, first run          thinking   analysis or planning running
//   hero       brand, about                working    an agent is running
//   point      invite, call to action      review     review or QA waiting
//   clipboard  a plan or analysis is ready approval   waiting for the person's decision
//   search     nothing matched the search  ship       release or deploy
//   sleepy     nothing here yet            celebrate  done, merged, shipped
//   oops       something failed            alert      production problem
//   offline    no connection to the computer or server
// Decorative: the text next to it carries the meaning, so it is hidden from
// assistive tech and never the only signal of a state.
export type MascotPose =
  | "alert"
  | "approval"
  | "celebrate"
  | "clipboard"
  | "hero"
  | "offline"
  | "oops"
  | "point"
  | "review"
  | "search"
  | "ship"
  | "sleepy"
  | "thinking"
  | "wave"
  | "working";

const poses = import.meta.glob<string>("@/assets/mascot/poses/*.webp", {
  eager: true,
  query: "?url",
  import: "default",
});

export function mascotPoseUrl(pose: MascotPose): string | undefined {
  return poses[`/src/assets/mascot/poses/${pose}.webp`];
}

export type MascotSize = "sm" | "md" | "lg" | "xl";

const sizeClasses: Record<MascotSize, string> = {
  sm: "h-16 w-16",
  md: "h-24 w-24",
  lg: "h-32 w-32",
  xl: "h-40 w-40",
};

interface MascotProps {
  pose: MascotPose;
  size?: MascotSize;
  className?: string;
}

export function Mascot({ pose, size = "md", className }: MascotProps) {
  const src = mascotPoseUrl(pose);
  if (!src) return null;
  return (
    <img
      src={src}
      alt=""
      aria-hidden
      draggable={false}
      data-mascot={pose}
      className={cn("pointer-events-none shrink-0 select-none object-contain", sizeClasses[size], className)}
    />
  );
}
