import { useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useTranslations } from "use-intl";
import { useModels, useRoute } from "@/api/queries";
import { Badge, EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * 模型路由（ui-spec §6.6）。
 *
 * 左侧列出全部可路由模型（`GET /models`：模型名 / 来源 / 成员数），
 * 右侧展示该模型解析出的成员链（`GET /routes/{model}`）。
 *
 * 成员链按 **priority 从高到低** 排序——PBR 是"数字大者优先"，
 * 与 `/logs` 的 attempts 顺序必须一致，否则这一页就失去了排障价值。
 */
export function Models() {
  const t = useTranslations("models");
  const tl = useTranslations("lanes");
  const tc = useTranslations("common");
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [filter, setFilter] = useState("");

  const selected = searchParams.get("model") ?? "";
  const models = useModels();
  const route = useRoute(selected);

  const list = useMemo(() => {
    const all = models.data?.items ?? [];
    const q = filter.trim().toLowerCase();
    const matched = q ? all.filter((item) => item.model.toLowerCase().includes(q)) : all;
    return [...matched].sort((a, b) => a.model.localeCompare(b.model));
  }, [models.data, filter]);

  // PBR 语义：priority 大者优先；同优先级按 weight 大者优先，再按渠道名稳定排序。
  const chain = useMemo(() => {
    const members = route.data?.members ?? [];
    return [...members].sort(
      (a, b) => b.priority - a.priority || b.weight - a.weight || a.channel.localeCompare(b.channel),
    );
  }, [route.data]);

  const select = (model: string) => {
    const next = new URLSearchParams(searchParams);
    if (model) next.set("model", model);
    else next.delete("model");
    setSearchParams(next);
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-lg font-semibold">{t("title")}</h1>
        <div className="flex items-center gap-2">
          <Input
            value={filter}
            onChange={(event) => setFilter(event.target.value)}
            placeholder={t("searchPlaceholder")}
            className="w-56"
          />
          <Button variant="outline" size="sm" onClick={() => void models.refetch()}>
            {tc("refresh")}
          </Button>
        </div>
      </div>

      <div className="grid gap-4 lg:grid-cols-[minmax(280px,360px)_1fr]">
        <section className="rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
          <header className="flex items-center justify-between border-b border-slate-200 px-3 py-2 dark:border-slate-800">
            <span className="text-sm font-medium">{t("model")}</span>
            <span className="text-xs text-slate-500">
              {models.data ? t("totalModels", { count: models.data.items.length }) : null}
            </span>
          </header>

          {models.isLoading ? (
            <div className="p-3">
              <Skeleton rows={4} />
            </div>
          ) : models.isError ? (
            <div className="p-3">
              <ErrorBox error={models.error} onRetry={() => void models.refetch()} />
            </div>
          ) : list.length === 0 ? (
            <div className="p-3">
              {models.data && models.data.items.length === 0 ? (
                <EmptyState
                  message={t("emptyGuide")}
                  action={<Button onClick={() => navigate("/channels")}>{t("goChannels")}</Button>}
                />
              ) : (
                <EmptyState message={t("noMatch")} />
              )}
            </div>
          ) : (
            <ul className="max-h-[65vh] overflow-auto p-1">
              {list.map((item) => {
                const active = item.model === selected;
                return (
                  <li key={item.model}>
                    <button
                      type="button"
                      onClick={() => select(item.model)}
                      className={cn(
                        "flex w-full items-center justify-between gap-2 rounded-md px-2.5 py-2 text-left text-sm transition-colors",
                        active
                          ? "bg-slate-900 text-white dark:bg-slate-100 dark:text-slate-900"
                          : "hover:bg-slate-100 dark:hover:bg-slate-800",
                      )}
                    >
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">{item.model}</span>
                      <Badge tone={item.source === "explicit" ? "ok" : "info"}>
                        {item.source === "explicit" ? t("sourceExplicit") : t("sourceImplicit")}
                      </Badge>
                      <span
                        className={cn(
                          "shrink-0 text-xs",
                          active ? "opacity-80" : "text-slate-500 dark:text-slate-400",
                        )}
                      >
                        {t("memberCount")} {item.member_count}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </section>

        <section className="rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
          <header className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200 px-3 py-2 dark:border-slate-800">
            <span className="text-sm font-medium">{t("memberChain")}</span>
            {selected ? (
              <Button
                variant="outline"
                size="sm"
                // `/lanes/new` 不在路由表里（新建是车道页上的弹窗），
                // 因此落到车道列表并把模型名带过去作为候选。
                onClick={() => navigate(`/lanes?model=${encodeURIComponent(selected)}`)}
              >
                {t("createLaneFromModel")}
              </Button>
            ) : null}
          </header>

          {!selected ? (
            <div className="p-3">
              <EmptyState message={t("selectHint")} />
            </div>
          ) : route.isLoading ? (
            <div className="p-3">
              <Skeleton rows={3} />
            </div>
          ) : route.isError ? (
            <div className="p-3">
              <ErrorBox error={route.error} onRetry={() => void route.refetch()} />
            </div>
          ) : (
            <div className="space-y-3 p-3">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-sm">{route.data?.model ?? selected}</span>
                <Badge tone={route.data?.source === "explicit" ? "ok" : "info"}>
                  {route.data?.source === "explicit" ? t("sourceExplicit") : t("sourceImplicit")}
                </Badge>
                {route.data?.mode ? (
                  <Badge tone="muted">
                    {tl("mode")}: {route.data.mode}
                  </Badge>
                ) : null}
                <span className="text-xs text-slate-500">{t("chainHint")}</span>
              </div>

              {chain.length === 0 ? (
                <EmptyState message={tl("noMembers")} />
              ) : (
                <ol className="space-y-2">
                  {chain.map((member, index) => (
                    <li
                      key={`${member.channel}:${member.upstream_model}:${index}`}
                      className="flex flex-wrap items-center gap-2 rounded-md border border-slate-200 px-3 py-2 text-sm dark:border-slate-800"
                    >
                      <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-slate-200 text-xs font-medium dark:bg-slate-700">
                        {index + 1}
                      </span>
                      <span className="font-medium">{member.channel}</span>
                      <span className="text-slate-400">→</span>
                      <span className="font-mono text-xs">{member.upstream_model}</span>
                      {member.public_alias ? (
                        <span className="text-xs text-slate-500">
                          {tl("publicAlias")}: <span className="font-mono">{member.public_alias}</span>
                        </span>
                      ) : null}
                      <span className="ml-auto flex items-center gap-2">
                        <Badge tone="muted">
                          {tl("priority")} {member.priority}
                        </Badge>
                        <Badge tone="muted">
                          {tl("weight")} {member.weight}
                        </Badge>
                      </span>
                    </li>
                  ))}
                </ol>
              )}
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
