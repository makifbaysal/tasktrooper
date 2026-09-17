interface LogoProps {
  className?: string;
}

// Ported verbatim from tasktrooper-site's components/Logo.tsx so the mark
// stays pixel-identical to the shipping macOS app icon (desktop/build/icon.png).
export function Logo({ className }: LogoProps) {
  return (
    <svg
      viewBox="0 0 100 130"
      className={className}
      aria-hidden="true"
      fill="currentColor"
      stroke="currentColor"
      strokeWidth={5}
      strokeLinejoin="round"
    >
      <path d="M6 30 L50 2 L94 30 V56 L50 30 L6 56 Z" />
      <path d="M6 66 L50 40 L94 66 V92 L50 66 L6 92 Z" />
      <path d="M6 102 L50 76 L94 102 V128 H6 Z" />
    </svg>
  );
}
