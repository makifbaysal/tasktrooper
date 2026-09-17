interface LogoProps {
  className?: string;
}

// The three-chevron "rank" mark from the shipping macOS app icon
// (desktop/build/icon.png), redrawn as a currentColor vector so it can sit
// inside any themed surface instead of a fixed gradient tile.
export function Logo({ className }: LogoProps) {
  return (
    <svg viewBox="0 0 100 130" fill="currentColor" className={className} aria-hidden="true">
      <path d="M50,8 L84,46 L50,26 L16,46 Z" />
      <path d="M50,52 L84,90 L50,70 L16,90 Z" />
      <path d="M50,96 L84,124 L16,124 Z" />
    </svg>
  );
}
