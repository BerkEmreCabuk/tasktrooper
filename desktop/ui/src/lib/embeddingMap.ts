// Pure helpers shared by the embedding-map panel and its UMAP web worker.
// Keep this module free of React and DOM APIs — it is imported inside a worker.

/** Bucket id used for every group that does not fit in the legend. */
export const OTHER_GROUP_ID = "__other__";

/** How many groups the legend lists before folding the rest into "other". */
export const MAX_LEGEND_GROUPS = 24;

export const DEFAULT_N_NEIGHBORS = 15;
export const DEFAULT_MIN_DIST = 0.1;
export const DEFAULT_LIMIT = 2000;
export const MIN_LIMIT = 100;
export const MAX_LIMIT = 5000;

/* -------------------------------------------------------------------------- */
/* Worker protocol                                                            */
/* -------------------------------------------------------------------------- */

export interface EmbeddingMapWorkerRequest {
  requestId: number;
  /** Row-major PCA-reduced vectors, `count` rows of `dims` values. */
  vectors: Float32Array;
  count: number;
  dims: number;
  nNeighbors: number;
  minDist: number;
}

export type EmbeddingMapWorkerResponse =
  | { type: "progress"; requestId: number; phase: "neighbors" | "layout"; ratio: number }
  | {
      type: "done";
      requestId: number;
      /** `2 * count` interleaved x/y pairs, already normalized into [0, 1]. */
      positions: Float32Array;
    }
  | { type: "error"; requestId: number; message: string };

/**
 * UMAP needs at least two neighbours and cannot use more than `count - 1`.
 * Callers pass the user-facing value; this keeps tiny datasets from blowing up.
 */
export function clampNeighbors(nNeighbors: number, count: number): number {
  const upper = Math.max(2, count - 1);
  return Math.max(2, Math.min(Math.round(nNeighbors), upper));
}

export function clampLimit(limit: number): number {
  if (!Number.isFinite(limit)) return DEFAULT_LIMIT;
  return Math.max(MIN_LIMIT, Math.min(MAX_LIMIT, Math.round(limit)));
}

export function clampMinDist(minDist: number): number {
  if (!Number.isFinite(minDist)) return DEFAULT_MIN_DIST;
  return Math.max(0.001, Math.min(0.99, minDist));
}

/**
 * Squash an arbitrary 2D layout into [0, 1] on both axes while preserving the
 * aspect ratio, so clusters keep their real shape. UMAP output has no fixed
 * range, so nothing here may assume one.
 */
export function normalizePositions(embedding: number[][]): Float32Array {
  const count = embedding.length;
  const out = new Float32Array(count * 2);
  if (count === 0) return out;
  if (count === 1) {
    out[0] = 0.5;
    out[1] = 0.5;
    return out;
  }

  let minX = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let minY = Number.POSITIVE_INFINITY;
  let maxY = Number.NEGATIVE_INFINITY;
  for (const row of embedding) {
    const x = Number.isFinite(row[0]) ? row[0] : 0;
    const y = Number.isFinite(row[1]) ? row[1] : 0;
    if (x < minX) minX = x;
    if (x > maxX) maxX = x;
    if (y < minY) minY = y;
    if (y > maxY) maxY = y;
  }

  const spanX = maxX - minX;
  const spanY = maxY - minY;
  const span = Math.max(spanX, spanY);
  if (!Number.isFinite(span) || span <= 0) {
    // Every point landed on the same spot — spread them on a small ring so the
    // user still sees "n points" rather than one dot.
    for (let i = 0; i < count; i++) {
      const angle = (i / count) * Math.PI * 2;
      out[i * 2] = 0.5 + Math.cos(angle) * 0.02;
      out[i * 2 + 1] = 0.5 + Math.sin(angle) * 0.02;
    }
    return out;
  }

  const offsetX = (span - spanX) / 2;
  const offsetY = (span - spanY) / 2;
  for (let i = 0; i < count; i++) {
    const row = embedding[i];
    const x = Number.isFinite(row[0]) ? row[0] : 0;
    const y = Number.isFinite(row[1]) ? row[1] : 0;
    out[i * 2] = (x - minX + offsetX) / span;
    out[i * 2 + 1] = (y - minY + offsetY) / span;
  }
  return out;
}

/* -------------------------------------------------------------------------- */
/* Categorical color scale                                                     */
/* -------------------------------------------------------------------------- */

// Validated categorical hues (see .ai design guidance / data-viz palette):
// fixed slot order, stepped separately for the light and dark surfaces.
const CATEGORICAL_LIGHT = [
  "#2a78d6", // blue
  "#eb6834", // orange
  "#1baf7a", // aqua
  "#eda100", // yellow
  "#e87ba4", // magenta
  "#008300", // green
  "#4a3aa7", // violet
  "#e34948", // red
];

const CATEGORICAL_DARK = [
  "#3987e5",
  "#d95926",
  "#199e70",
  "#c98500",
  "#d55181",
  "#008300",
  "#9085e9",
  "#e66767",
];

// Lightness offsets (OKLab L) applied once the eight hues are exhausted, so a
// cycled hue never looks identical to its neighbour on the palette wheel.
// The light surface steps mostly darker and the dark surface mostly lighter, so
// a cycled color never drifts into its own background.
const LIGHTNESS_TIERS = {
  light: [0, -0.13, 0.09, -0.24, 0.04, -0.19, -0.07],
  dark: [0, 0.13, -0.09, 0.24, -0.04, 0.19, 0.07],
} as const;

// Keep cycled steps clear of the surface they are drawn on.
const LIGHTNESS_BOUNDS = {
  light: { min: 0.3, max: 0.7 },
  dark: { min: 0.5, max: 0.9 },
} as const;

export type EmbeddingMapTheme = "light" | "dark";

/** Neutral used for the folded "other" bucket. */
export function otherGroupColor(theme: EmbeddingMapTheme): string {
  return theme === "dark" ? "#7c7c86" : "#9a9aa4";
}

function srgbToLinear(channel: number): number {
  return channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4);
}

function linearToSrgb(channel: number): number {
  return channel <= 0.0031308 ? channel * 12.92 : 1.055 * Math.pow(channel, 1 / 2.4) - 0.055;
}

interface OkLab {
  l: number;
  a: number;
  b: number;
}

function hexToOkLab(hex: string): OkLab {
  const value = hex.replace("#", "");
  const r = srgbToLinear(parseInt(value.slice(0, 2), 16) / 255);
  const g = srgbToLinear(parseInt(value.slice(2, 4), 16) / 255);
  const b = srgbToLinear(parseInt(value.slice(4, 6), 16) / 255);

  const lCone = Math.cbrt(0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b);
  const mCone = Math.cbrt(0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b);
  const sCone = Math.cbrt(0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b);

  return {
    l: 0.2104542553 * lCone + 0.793617785 * mCone - 0.0040720468 * sCone,
    a: 1.9779984951 * lCone - 2.428592205 * mCone + 0.4505937099 * sCone,
    b: 0.0259040371 * lCone + 0.7827717662 * mCone - 0.808675766 * sCone,
  };
}

function toHexChannel(value: number): string {
  const clamped = Math.max(0, Math.min(255, Math.round(value * 255)));
  return clamped.toString(16).padStart(2, "0");
}

function okLabToHex({ l, a, b }: OkLab): string {
  const lCone = (l + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const mCone = (l - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const sCone = (l - 0.0894841775 * a - 1.291485548 * b) ** 3;

  const r = linearToSrgb(4.0767416621 * lCone - 3.3077115913 * mCone + 0.2309699292 * sCone);
  const g = linearToSrgb(-1.2684380046 * lCone + 2.6097574011 * mCone - 0.3413193965 * sCone);
  const bl = linearToSrgb(-0.0041960863 * lCone - 0.7034186147 * mCone + 1.707614701 * sCone);

  return `#${toHexChannel(r)}${toHexChannel(g)}${toHexChannel(bl)}`;
}

/**
 * Deterministic color for the Nth distinct group. Slots 0-7 are the validated
 * hues in fixed order; beyond that the hue cycles while the OKLab lightness
 * steps, so adjacent indices stay distinguishable.
 */
export function groupColorAt(index: number, theme: EmbeddingMapTheme): string {
  const palette = theme === "dark" ? CATEGORICAL_DARK : CATEGORICAL_LIGHT;
  const hex = palette[index % palette.length];
  const tiers = LIGHTNESS_TIERS[theme];
  const tier = tiers[Math.floor(index / palette.length) % tiers.length];
  if (tier === 0) return hex;
  const lab = hexToOkLab(hex);
  const bounds = LIGHTNESS_BOUNDS[theme];
  const l = Math.max(bounds.min, Math.min(bounds.max, lab.l + tier));
  return okLabToHex({ ...lab, l });
}

export interface EmbeddingGroup {
  id: string;
  label: string;
  count: number;
  color: string;
}

export interface EmbeddingGroupScale {
  /** Every distinct group, sorted by label — the color assignment order. */
  groups: EmbeddingGroup[];
  /** Legend rows: the biggest groups first, capped at `MAX_LEGEND_GROUPS`. */
  legend: EmbeddingGroup[];
  /** Groups folded into the "other" legend row (empty when nothing folded). */
  otherGroupIds: Set<string>;
  colorByGroupId: Map<string, string>;
}

/**
 * Build the categorical scale. Sorting the distinct labels before assigning
 * slots is what keeps a file's color stable across re-projections.
 */
export function buildGroupScale(
  entries: { groupId: string; label: string }[],
  theme: EmbeddingMapTheme,
): EmbeddingGroupScale {
  const counts = new Map<string, { label: string; count: number }>();
  for (const entry of entries) {
    const existing = counts.get(entry.groupId);
    if (existing) {
      existing.count += 1;
    } else {
      counts.set(entry.groupId, { label: entry.label, count: 1 });
    }
  }

  const groups: EmbeddingGroup[] = [...counts.entries()]
    .map(([id, value]) => ({ id, label: value.label, count: value.count, color: "" }))
    .sort((a, b) => a.label.localeCompare(b.label) || a.id.localeCompare(b.id));

  const colorByGroupId = new Map<string, string>();
  groups.forEach((group, index) => {
    group.color = groupColorAt(index, theme);
    colorByGroupId.set(group.id, group.color);
  });

  const byCount = [...groups].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
  const legend = byCount.slice(0, MAX_LEGEND_GROUPS);
  const otherGroupIds = new Set(byCount.slice(MAX_LEGEND_GROUPS).map((group) => group.id));

  return { groups, legend, otherGroupIds, colorByGroupId };
}

/* -------------------------------------------------------------------------- */
/* Spatial index for hover hit-testing                                         */
/* -------------------------------------------------------------------------- */

/**
 * Uniform grid over the normalized [0, 1] layout. Hover lookups only scan the
 * cells within the query radius, so mousemove stays cheap at 5000 points.
 */
export class PointGrid {
  private readonly cells: number[][];
  private readonly resolution: number;
  private readonly positions: Float32Array;

  constructor(positions: Float32Array, resolution?: number) {
    this.positions = positions;
    const count = positions.length / 2;
    this.resolution = Math.max(1, resolution ?? Math.ceil(Math.sqrt(Math.max(count, 1)) / 2));
    this.cells = Array.from({ length: this.resolution * this.resolution }, () => []);
    for (let i = 0; i < count; i++) {
      const cell = this.cellIndex(positions[i * 2], positions[i * 2 + 1]);
      this.cells[cell].push(i);
    }
  }

  private cellIndex(x: number, y: number): number {
    const cx = Math.max(0, Math.min(this.resolution - 1, Math.floor(x * this.resolution)));
    const cy = Math.max(0, Math.min(this.resolution - 1, Math.floor(y * this.resolution)));
    return cy * this.resolution + cx;
  }

  /** Index of the closest point within `radius`, or -1. Coordinates are [0, 1]. */
  nearest(x: number, y: number, radius: number): number {
    const span = Math.max(1, Math.ceil(radius * this.resolution));
    const cx = Math.max(0, Math.min(this.resolution - 1, Math.floor(x * this.resolution)));
    const cy = Math.max(0, Math.min(this.resolution - 1, Math.floor(y * this.resolution)));
    const radiusSq = radius * radius;

    let best = -1;
    let bestDistSq = radiusSq;
    for (let gy = cy - span; gy <= cy + span; gy++) {
      if (gy < 0 || gy >= this.resolution) continue;
      for (let gx = cx - span; gx <= cx + span; gx++) {
        if (gx < 0 || gx >= this.resolution) continue;
        for (const index of this.cells[gy * this.resolution + gx]) {
          const dx = this.positions[index * 2] - x;
          const dy = this.positions[index * 2 + 1] - y;
          const distSq = dx * dx + dy * dy;
          if (distSq <= bestDistSq) {
            bestDistSq = distSq;
            best = index;
          }
        }
      }
    }
    return best;
  }
}
