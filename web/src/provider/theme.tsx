import { useEffect } from "react";
import { useSettingStore, type Theme } from "@/stores/setting";

function resolve(theme: Theme): "light" | "dark" {
  if (theme !== "system") return theme;
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

/** ThemeProvider 把主题写到 <html> 的 class 上（Tailwind 4 深色模式）。 */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const theme = useSettingStore((state) => state.theme);

  useEffect(() => {
    const apply = () => {
      const resolved = resolve(theme);
      document.documentElement.classList.toggle("dark", resolved === "dark");
      document.documentElement.style.colorScheme = resolved;
    };
    apply();
    if (theme !== "system") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [theme]);

  return children;
}
