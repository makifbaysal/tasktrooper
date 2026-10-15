import { nativeImage, type NativeImage } from "electron";

/**
 * The tray glyphs: the logo's three chevrons, drawn into a bitmap at startup
 * rather than shipped as .png assets.
 *
 * Two reasons, both practical. A macOS template image is pure black plus an
 * alpha channel — the system recolours it for light mode, dark mode,
 * highlighted and inactive — so the "art" here is one alpha mask per state,
 * and the mark is three polygons already written down in the UI's logo. And
 * because the icons are alpha-only, state cannot be shown with colour: a
 * template image tinted green would come out black anyway. So the states
 * differ in how much of the mark is solid, which also still works for someone
 * who cannot distinguish the colours.
 *
 *   running   all three chevrons solid      — attached and healthy
 *   degraded  the top one faint             — processes up, tunnel not attached
 *   stopped   all three faint               — nothing running
 */

export type TrayGlyph = "running" | "degraded" | "stopped";

/**
 * How the glyph is coloured. Only macOS recolours a template image for the bar
 * it sits on; everywhere else the pixels are drawn as they are, and a black
 * glyph on Windows' dark taskbar or GNOME's top bar is invisible.
 *
 *   template  black + alpha, recoloured by macOS
 *   light     white, for a dark panel
 *   dark      black, for a light panel
 */
export type TrayTone = "template" | "light" | "dark";

/**
 * The tone for this platform's panel.
 *
 * Windows reports the taskbar's own theme separately from the apps' (the
 * default on Windows 10 is a dark taskbar with light apps), so that is the one
 * read. On Linux the panel's colour is the desktop's decision rather than the
 * GTK theme's: GNOME's top bar is dark in light mode too, as are most others,
 * so only KDE — whose panel follows the colour scheme — gets the theme's answer.
 */
export function trayTone(
  platform: NodeJS.Platform,
  theme: { systemDark: boolean; appDark: boolean },
  desktop: string = process.env.XDG_CURRENT_DESKTOP ?? "",
): TrayTone {
  if (platform === "darwin") return "template";
  if (platform === "win32") return theme.systemDark ? "light" : "dark";
  if (/kde/i.test(desktop)) return theme.appDark ? "light" : "dark";
  return "light";
}

/** Points, not pixels; the @2x buffer is generated at twice this. */
const SIZE = 18;

type Point = readonly [number, number];

/**
 * ui/src/assets/logo.tsx's paths in its 100×130 box, without the 5-unit
 * stroke it draws them with: at 18pt that stroke closes the gaps between the
 * chevrons to half a pixel and the mark reads as one block.
 */
const CHEVRONS: readonly (readonly Point[])[] = [
  [[6, 30], [50, 2], [94, 30], [94, 56], [50, 30], [6, 56]],
  [[6, 66], [50, 40], [94, 66], [94, 92], [50, 66], [6, 92]],
  [[6, 102], [50, 76], [94, 102], [94, 128], [6, 128]],
];
const MARK = { left: 6, top: 2, width: 88, height: 126 };

/** Faint enough to read as off next to a solid chevron, still there to find. */
const FAINT = 0.38;

/** Each chevron's opacity, top to bottom. */
const SHADE: Record<TrayGlyph, readonly number[]> = {
  running: [1, 1, 1],
  degraded: [FAINT, 1, 1],
  stopped: [FAINT, FAINT, FAINT],
};

function inside(polygon: readonly Point[], x: number, y: number): boolean {
  let hit = false;
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i, i += 1) {
    const [xi, yi] = polygon[i]!;
    const [xj, yj] = polygon[j]!;
    if (yi > y !== yj > y && x < ((xj - xi) * (y - yi)) / (yj - yi) + xi) hit = !hit;
  }
  return hit;
}

/**
 * Draws one glyph as BGRA and hands it to nativeImage.createFromBitmap.
 * Supersampled 4× and averaged, which is the whole antialiasing strategy — at
 * 18pt hard-edged chevrons read as a lump. The mark is 8/9 of the height, so
 * its flat bottom lands on a pixel edge at both scales.
 */
export function drawGlyph(glyph: TrayGlyph, size: number, tone: TrayTone): Buffer {
  const buffer = Buffer.alloc(size * size * 4);
  const scale = (size * 8) / 9 / MARK.height;
  const left = (size - MARK.width * scale) / 2;
  const top = (size - MARK.height * scale) / 2;
  const shade = SHADE[glyph];
  const samples = 4;

  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      let coverage = 0;
      for (let sy = 0; sy < samples; sy += 1) {
        for (let sx = 0; sx < samples; sx += 1) {
          const mx = MARK.left + (x + (sx + 0.5) / samples - left) / scale;
          const my = MARK.top + (y + (sy + 0.5) / samples - top) / scale;
          const chevron = CHEVRONS.findIndex((polygon) => inside(polygon, mx, my));
          if (chevron >= 0) coverage += shade[chevron]!;
        }
      }
      const alpha = Math.round((coverage / (samples * samples)) * 255);
      const offset = (y * size + x) * 4;
      // BGRA, premultiplied: black, or white, at the computed coverage.
      const channel = tone === "light" ? alpha : 0;
      buffer[offset] = channel;
      buffer[offset + 1] = channel;
      buffer[offset + 2] = channel;
      buffer[offset + 3] = alpha;
    }
  }
  return buffer;
}

const cache = new Map<string, NativeImage>();

export function trayIcon(glyph: TrayGlyph, tone: TrayTone = "template"): NativeImage {
  const key = `${glyph}:${tone}`;
  const cached = cache.get(key);
  if (cached) return cached;

  const image = nativeImage.createFromBitmap(drawGlyph(glyph, SIZE, tone), { width: SIZE, height: SIZE, scaleFactor: 1 });
  image.addRepresentation({
    width: SIZE * 2,
    height: SIZE * 2,
    scaleFactor: 2,
    buffer: drawGlyph(glyph, SIZE * 2, tone),
  });
  // Without this the menu bar shows a black blob that stays black when the
  // bar inverts. It is what makes the icon behave like every other one there.
  if (tone === "template") image.setTemplateImage(true);

  cache.set(key, image);
  return image;
}
