import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

/**
 * Formatters and the `cn` helper, kept identical to `web/src/lib/utils.ts`
 * where they overlap so the consolidation pass can fold the two files
 * together rather than reconcile them.
 */

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/**
 * Timestamps in the status surfaces are always "how long ago", never a clock
 * time. Every one of them answers a question about something running right
 * now — when the backend last exited, when the next restart is due — and
 * "14:32" makes the reader do the subtraction.
 */
export function timeAgo(epochMs: number | undefined, now = Date.now()): string {
  if (epochMs === undefined) return "—";
  const seconds = Math.max(0, Math.round((now - epochMs) / 1000));
  if (seconds < 5) return "just now";
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h ago`;
}

/** The same, for an ISO string off the wire. */
export function timeAgoISO(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return "—";
  const parsed = Date.parse(iso);
  return Number.isNaN(parsed) ? "—" : timeAgo(parsed, now);
}

/** A countdown to a future epoch, for "retrying in 8s". */
export function timeUntil(epochMs: number | undefined, now = Date.now()): string {
  if (epochMs === undefined) return "—";
  const seconds = Math.max(0, Math.round((epochMs - now) / 1000));
  if (seconds < 60) return `${seconds}s`;
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

export function formatClock(epochMs: number): string {
  const d = new Date(epochMs);
  const pad = (n: number): string => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** Same units and rounding as web's formatFileSize, extended for disk space. */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const size = bytes / Math.pow(1024, i);
  return `${size.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}
