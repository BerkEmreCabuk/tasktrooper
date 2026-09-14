import { nativeImage, type NativeImage } from "electron";

/**
 * The tray glyphs, drawn into a bitmap at startup rather than shipped as .png
 * assets.
 *
 * Two reasons, both practical. A macOS template image is pure black plus an
 * alpha channel — the system recolours it for light mode, dark mode,
 * highlighted and inactive — so the "art" here is one alpha mask per state and
 * checking three antialiased PNGs into the repo to express three circles is a
 * poor trade. And because the icons are alpha-only, state cannot be shown with
 * colour: a template image tinted green would come out black anyway. So the
 * three states differ in SHAPE, which also happens to be the version that
 * still works for someone who cannot distinguish the colours.
 *
 *   running   ● a filled disc      — attached and healthy
 *   degraded  ◐ a half disc        — processes up, tunnel not attached
 *   stopped   ○ a hollow ring      — nothing running
 */

export type TrayGlyph = "running" | "degraded" | "stopped";

/** Points, not pixels; the @2x buffer is generated at twice this. */
const SIZE = 18;

/**
 * Draws one glyph as BGRA and hands it to nativeImage.createFromBitmap.
 * Supersampled 4× and averaged, which is the whole antialiasing strategy — at
 * 18pt a hard-edged circle reads as a lump.
 */
function drawGlyph(glyph: TrayGlyph, size: number): Buffer {
  const buffer = Buffer.alloc(size * size * 4);
  const center = (size - 1) / 2;
  const outer = size * 0.40;
  const inner = size * 0.24;
  const samples = 4;

  for (let y = 0; y < size; y += 1) {
    for (let x = 0; x < size; x += 1) {
      let hits = 0;
      for (let sy = 0; sy < samples; sy += 1) {
        for (let sx = 0; sx < samples; sx += 1) {
          const px = x + (sx + 0.5) / samples - 0.5;
          const py = y + (sy + 0.5) / samples - 0.5;
          const dx = px - center;
          const dy = py - center;
          const distance = Math.sqrt(dx * dx + dy * dy);
          if (distance > outer) continue;
          if (glyph === "running") hits += 1;
          else if (glyph === "stopped") {
            if (distance >= inner) hits += 1;
          } else if (distance >= inner || dx <= 0) hits += 1;
        }
      }
      const alpha = Math.round((hits / (samples * samples)) * 255);
      const offset = (y * size + x) * 4;
      // BGRA, premultiplied: black at the computed coverage.
      buffer[offset] = 0;
      buffer[offset + 1] = 0;
      buffer[offset + 2] = 0;
      buffer[offset + 3] = alpha;
    }
  }
  return buffer;
}

const cache = new Map<TrayGlyph, NativeImage>();

export function trayIcon(glyph: TrayGlyph): NativeImage {
  const cached = cache.get(glyph);
  if (cached) return cached;

  const image = nativeImage.createFromBitmap(drawGlyph(glyph, SIZE), { width: SIZE, height: SIZE, scaleFactor: 1 });
  image.addRepresentation({
    width: SIZE * 2,
    height: SIZE * 2,
    scaleFactor: 2,
    buffer: drawGlyph(glyph, SIZE * 2),
  });
  // Without this the menu bar shows a black blob that stays black when the
  // bar inverts. It is what makes the icon behave like every other one there.
  image.setTemplateImage(true);

  cache.set(glyph, image);
  return image;
}
