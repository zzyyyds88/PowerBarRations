import { create } from "zustand";
import { persist } from "zustand/middleware";

/** 语言（ui-spec §1：zh-Hans / zh-Hant / en 三语）。 */
export type Locale = "zh_hans" | "zh_hant" | "en";

/** 主题（ui-spec §6.9 外观分节）。 */
export type Theme = "light" | "dark" | "system";

interface SettingState {
  locale: Locale;
  theme: Theme;
  setLocale: (locale: Locale) => void;
  setTheme: (theme: Theme) => void;
}

/**
 * 外观与语言偏好。持久化到 localStorage，键名前缀为 pbr_*
 * （ui-spec §2：品牌替换范围含 localStorage 键名前缀）。
 */
export const useSettingStore = create<SettingState>()(
  persist(
    (set) => ({
      locale: "zh_hans",
      theme: "system",
      setLocale: (locale) => set({ locale }),
      setTheme: (theme) => set({ theme }),
    }),
    { name: "pbr_settings" },
  ),
);
