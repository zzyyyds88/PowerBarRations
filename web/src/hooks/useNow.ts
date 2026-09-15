import { useEffect, useState } from "react";

/**
 * useNow 返回一个按 intervalMs 自动前进的"当前时间"（Unix 毫秒）。
 *
 * 为什么需要它：在 render 里直接调用 `Date.now()` 属于不纯调用（React Compiler
 * 会报 "Cannot call impure function during render"），而且会让同一份数据在不同
 * 渲染间产生不同的查询键。把"现在"放进 state，由定时器推进，渲染就变成纯函数。
 */
export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);

  return now;
}
