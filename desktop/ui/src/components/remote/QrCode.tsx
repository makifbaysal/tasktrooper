import { useMemo } from "react";
import { encodeQr } from "@/lib/qr";

interface QrCodeProps {
  value: string;
  label: string;
  className?: string;
}

const QUIET = 4;

/**
 * An inline SVG QR code: black modules on a white square, whatever the theme,
 * because a scanner needs the contrast and a quiet zone, not the app's colours.
 */
export function QrCode({ value, label, className }: QrCodeProps) {
  const matrix = useMemo(() => {
    try {
      return encodeQr(value);
    } catch {
      return null;
    }
  }, [value]);
  if (!matrix) return null;
  const size = matrix.length + QUIET * 2;
  const path: string[] = [];
  matrix.forEach((row, y) => {
    let x = 0;
    while (x < row.length) {
      if (!row[x]) {
        x += 1;
        continue;
      }
      let end = x;
      while (end < row.length && row[end]) end += 1;
      path.push(`M${x + QUIET} ${y + QUIET}h${end - x}v1h-${end - x}z`);
      x = end;
    }
  });
  return (
    <svg
      role="img"
      aria-label={label}
      viewBox={`0 0 ${size} ${size}`}
      shapeRendering="crispEdges"
      className={className ?? "h-56 w-56 rounded-lg"}
    >
      <rect width={size} height={size} fill="#ffffff" />
      <path d={path.join("")} fill="#000000" />
    </svg>
  );
}

/** True when `value` fits in a QR code; the pairing screen falls back to the typed code otherwise. */
export function canDrawQr(value: string): boolean {
  try {
    encodeQr(value);
    return true;
  } catch {
    return false;
  }
}
