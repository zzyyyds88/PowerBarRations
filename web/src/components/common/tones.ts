/**
 * 状态 → 徽标颜色的映射（单独成文件，避免组件文件导出非组件符号，
 * 否则 React Fast Refresh 失效）。
 */

export type Tone = "ok" | "warn" | "err" | "muted" | "info";

/**
 * 尝试状态 → 颜色（ui-spec §6.7 要求区分 cooldown/circuit_break/skipped）。
 * 这是日志页排障价值的核心：被跳过的成员必须与"失败"在视觉上可区分。
 */
export function attemptTone(status: string): Tone {
  switch (status) {
    case "success":
      return "ok";
    case "failed":
      return "err";
    case "cooldown":
      return "warn";
    case "circuit_break":
      return "err";
    case "skipped":
      return "info";
    default:
      return "muted";
  }
}

/** 熔断状态 → 颜色。 */
export function circuitTone(state: string): Tone {
  switch (state) {
    case "closed":
      return "ok";
    case "half_open":
      return "warn";
    case "open":
      return "err";
    default:
      return "muted";
  }
}
