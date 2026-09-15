import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

/** cn 合并 Tailwind 类名（CVA + tailwind-merge，ui-spec §1）。 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

/** 把毫秒时间戳格式化为本地时间；0/空表示"无"。 */
export function formatTime(ms: number | null | undefined): string {
  if (!ms) return "—";
  return new Date(ms).toLocaleString();
}

/** 倒计时秒数（用于冷却/熔断展示）。 */
export function secondsUntil(ms: number | null | undefined): number {
  if (!ms) return 0;
  return Math.max(0, Math.ceil((ms - Date.now()) / 1000));
}

/** 紧凑数字：1234 → 1.2K。 */
export function compactNumber(value: number | null | undefined): string {
  if (value === null || value === undefined) return "0";
  if (Math.abs(value) >= 1_000_000) return (value / 1_000_000).toFixed(1) + "M";
  if (Math.abs(value) >= 1_000) return (value / 1_000).toFixed(1) + "K";
  return String(value);
}
