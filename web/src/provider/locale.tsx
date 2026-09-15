import { useEffect, type ReactNode } from "react";
import { IntlProvider } from "use-intl";
import { useSettingStore, type Locale } from "@/stores/setting";

import zhHansMessages from "@/locales/zh_hans.json";
import zhHantMessages from "@/locales/zh_hant.json";
import enMessages from "@/locales/en.json";

const messages: Record<Locale, typeof zhHansMessages> = {
  zh_hans: zhHansMessages,
  zh_hant: zhHantMessages,
  en: enMessages,
};

const languageTags: Record<Locale, string> = {
  zh_hans: "zh-Hans",
  zh_hant: "zh-Hant",
  en: "en",
};

/** LocaleProvider 注入 use-intl（ui-spec §1：三语与蓝本一致）。 */
export function LocaleProvider({ children }: { children: ReactNode }) {
  const locale = useSettingStore((state) => state.locale);

  useEffect(() => {
    document.documentElement.lang = languageTags[locale];
  }, [locale]);

  return (
    <IntlProvider locale={languageTags[locale]} messages={messages[locale]} timeZone="Asia/Shanghai">
      {children}
    </IntlProvider>
  );
}
