import { useState } from "react";
import { Check, Copy } from "lucide-react";
import { useTranslations } from "use-intl";
import { Button } from "@/components/ui/button";

/** CopyButton：一次性明文/摘要的复制入口（ui-spec §6.1/§6.9）。 */
export function CopyButton({ value, className }: { value: string; className?: string }) {
  const t = useTranslations("common");
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      setCopied(false);
    }
  };

  return (
    <Button type="button" variant="outline" size="sm" className={className} onClick={copy}>
      {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
      {copied ? t("copied") : t("copy")}
    </Button>
  );
}
