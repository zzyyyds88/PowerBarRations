import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useTranslations } from "use-intl";
import { toast } from "sonner";
import { AlertTriangle, Plus, Send, Trash2 } from "lucide-react";
import { useModels } from "@/api/queries";
import { Button } from "@/components/ui/button";
import { Field, Input, Select, Switch, Textarea } from "@/components/ui/input";
import { EmptyState, ErrorBox, Skeleton } from "@/components/common/AsyncState";
import { cn } from "@/lib/utils";

/**
 * Playground / 试打台（ui-spec §6.10）。
 *
 * 这一页走的是**模型面** `/v1/chat/completions`，不是管理面 `/api/v1`：
 * 因此认证用的是客户端密钥（独立的 localStorage 键），而不是管理密钥。
 *
 * 验收要点：
 * - 可见真实服务成员：响应头 `X-Served-By`（`channel=<id>:<name>, model=<upstream>`）原样展示；
 * - 思考参数 `enable_thinking` / `reasoning_effort` 成对下发，半关时把上游 400 的原文明确显示。
 */

const PLAYGROUND_KEY_STORAGE = "pbr.playgroundKey";

type Role = "user" | "assistant" | "system";

interface ChatMessage {
  role: Role;
  content: string;
}

interface Usage {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
}

interface ChatChoice {
  message?: { content?: string | null; reasoning_content?: string | null };
}

interface ChatCompletion {
  choices?: ChatChoice[];
  usage?: Usage;
}

interface ChatDelta {
  content?: string | null;
  reasoning_content?: string | null;
}

interface ChatCompletionChunk {
  choices?: { delta?: ChatDelta }[];
  usage?: Usage;
}

interface RequestFailure {
  status: number;
  message: string;
  raw: string;
}

function readPlaygroundKey(): string {
  try {
    return localStorage.getItem(PLAYGROUND_KEY_STORAGE) ?? "";
  } catch {
    return "";
  }
}

/** 从模型面错误体里挖出可读信息：OpenAI 风格 `{error:{message}}` 或裸文本。 */
function extractErrorMessage(status: number, body: string): string {
  const fallback = body.trim() || `HTTP ${status}`;
  try {
    const parsed = JSON.parse(body) as {
      error?: { message?: string } | string;
      message?: string;
    };
    const nested = parsed.error;
    if (typeof nested === "string" && nested) return nested;
    if (nested && typeof nested === "object" && nested.message) return nested.message;
    if (parsed.message) return parsed.message;
  } catch {
    // 非 JSON 错误体：原样展示
  }
  return fallback;
}

export function Playground() {
  const t = useTranslations("playground");
  const tc = useTranslations("common");
  const navigate = useNavigate();
  const modelsQuery = useModels();

  const [clientKey, setClientKey] = useState(readPlaygroundKey);
  const [model, setModel] = useState("");
  const [stream, setStream] = useState(true);
  const [thinking, setThinking] = useState(false);
  const [effort, setEffort] = useState("low");
  const [temperature, setTemperature] = useState("0.7");
  const [maxTokens, setMaxTokens] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([{ role: "user", content: "" }]);

  const [output, setOutput] = useState("");
  const [reasoning, setReasoning] = useState("");
  const [usage, setUsage] = useState<Usage | null>(null);
  const [servedBy, setServedBy] = useState("");
  const [ttft, setTtft] = useState<number | null>(null);
  const [failure, setFailure] = useState<RequestFailure | null>(null);
  const [running, setRunning] = useState(false);
  const ttftRef = useRef<number | null>(null);
  const startedAtRef = useRef(0);

  useEffect(() => {
    try {
      if (clientKey) {
        localStorage.setItem(PLAYGROUND_KEY_STORAGE, clientKey);
      } else {
        localStorage.removeItem(PLAYGROUND_KEY_STORAGE);
      }
    } catch {
      // 隐私模式下 localStorage 不可写：不阻断试打
    }
  }, [clientKey]);

  const models = modelsQuery.data?.items ?? [];

  // 默认模型用**派生值**而不是 effect 里 setState：effect 里同步 setState 会被
  // React Compiler 判为级联渲染。用户未选时回落到清单第一项。
  const effectiveModel = model || models[0]?.model || "";

  const updateMessage = (index: number, patch: Partial<ChatMessage>) => {
    setMessages((prev) => prev.map((item, i) => (i === index ? { ...item, ...patch } : item)));
  };

  const applyChunk = (chunk: ChatCompletionChunk) => {
    const delta = chunk.choices?.[0]?.delta;
    if (delta?.reasoning_content) {
      setReasoning((prev) => prev + delta.reasoning_content);
    }
    if (delta?.content) {
      if (ttftRef.current === null) {
        ttftRef.current = performance.now();
        setTtft(ttftRef.current - startedAtRef.current);
      }
      setOutput((prev) => prev + delta.content);
    }
    if (chunk.usage) setUsage(chunk.usage);
  };

  /** 统一解析非流式响应体；流式被上游降级时也走这里。 */
  const applyFullResponse = (data: ChatCompletion) => {
    const choice = data.choices?.[0];
    setOutput(choice?.message?.content ?? "");
    setReasoning(choice?.message?.reasoning_content ?? "");
    setUsage(data.usage ?? null);
    setTtft(performance.now() - startedAtRef.current);
  };

  const send = async () => {
    const key = clientKey.trim();
    if (!key) {
      toast.error(t("clientKeyRequired"));
      return;
    }
    if (!effectiveModel) {
      toast.error(t("modelRequired"));
      return;
    }
    const sent = messages.filter((message) => message.content.trim() !== "");
    if (sent.length === 0) {
      toast.error(t("messagesRequired"));
      return;
    }

    setRunning(true);
    setFailure(null);
    setOutput("");
    setReasoning("");
    setUsage(null);
    setServedBy("");
    setTtft(null);
    ttftRef.current = null;
    startedAtRef.current = performance.now();

    const body: Record<string, unknown> = { model: effectiveModel, messages: sent, stream };
    const temp = Number.parseFloat(temperature);
    if (Number.isFinite(temp)) body.temperature = temp;
    const max = Number.parseInt(maxTokens, 10);
    if (Number.isFinite(max) && max > 0) body.max_tokens = max;
    // 思考参数成对下发：只设一个上游会回 400。
    if (thinking) {
      body.enable_thinking = true;
      body.reasoning_effort = effort;
    }

    try {
      const response = await fetch("/v1/chat/completions", {
        method: "POST",
        headers: {
          Authorization: `Bearer ${key}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(body),
      });

      setServedBy(response.headers.get("X-Served-By") ?? "");

      if (!response.ok) {
        const raw = await response.text();
        setFailure({ status: response.status, message: extractErrorMessage(response.status, raw), raw });
        toast.error(t("requestFailed", { status: response.status }));
        return;
      }

      const contentType = response.headers.get("Content-Type") ?? "";
      const isEventStream = contentType.includes("event-stream") || contentType.trim() === "";
      if (stream && response.body && isEventStream) {
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        const handleLine = (rawLine: string) => {
          const line = rawLine.trim();
          if (!line.startsWith("data:")) return;
          const payload = line.slice(5).trim();
          if (!payload || payload === "[DONE]") return;
          try {
            applyChunk(JSON.parse(payload) as ChatCompletionChunk);
          } catch {
            // 半截 JSON 或心跳：忽略
          }
        };
        let buffer = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          const lines = buffer.split("\n");
          buffer = lines.pop() ?? "";
          for (const rawLine of lines) handleLine(rawLine);
        }
        handleLine(buffer);
        if (ttftRef.current === null) {
          // 流结束都没拿到内容块：TTFT 记为整体耗时，避免显示空值。
          setTtft(performance.now() - startedAtRef.current);
        }
      } else {
        // 非流式请求，或上游忽略了 stream：按完整响应体解析。
        applyFullResponse((await response.json()) as ChatCompletion);
      }
    } catch (error) {
      setFailure({
        status: 0,
        message: error instanceof Error ? error.message : String(error),
        raw: "",
      });
    } finally {
      setRunning(false);
    }
  };

  const clear = () => {
    setMessages([{ role: "user", content: "" }]);
    setOutput("");
    setReasoning("");
    setUsage(null);
    setServedBy("");
    setTtft(null);
    setFailure(null);
  };

  return (
    <div className="space-y-4">
      <h1 className="text-lg font-semibold">{t("title")}</h1>

      <div className="grid gap-4 lg:grid-cols-2">
        <div className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <Field label={t("clientKey")} hint={t("clientKeyHint")}>
            <Input
              type="password"
              value={clientKey}
              placeholder="pbr-..."
              onChange={(event) => setClientKey(event.target.value)}
            />
          </Field>

          <Field label={t("lane")} hint={t("laneHint")}>
            {modelsQuery.isLoading ? (
              <Skeleton rows={1} />
            ) : modelsQuery.isError ? (
              <ErrorBox error={modelsQuery.error} onRetry={() => void modelsQuery.refetch()} />
            ) : models.length === 0 ? (
              <EmptyState
                message={t("emptyGuide")}
                action={
                  <Button size="sm" onClick={() => navigate("/channels")}>
                    {t("goToChannels")}
                  </Button>
                }
              />
            ) : (
              <Select value={model} onChange={(event) => setModel(event.target.value)}>
                <option value="">—</option>
                {models.map((item) => (
                  <option key={item.model} value={item.model}>
                    {item.model} ({item.member_count})
                  </option>
                ))}
              </Select>
            )}
          </Field>

          <div className="grid gap-3 md:grid-cols-2">
            <Field label={t("temperature")}>
              <Input
                inputMode="decimal"
                value={temperature}
                onChange={(event) => setTemperature(event.target.value)}
              />
            </Field>
            <Field label={t("maxTokens")} hint={t("maxTokensHint")}>
              <Input
                inputMode="numeric"
                value={maxTokens}
                onChange={(event) => setMaxTokens(event.target.value)}
              />
            </Field>
          </div>

          <div className="flex items-center justify-between rounded-md border border-slate-200 px-3 py-2 dark:border-slate-800">
            <span className="text-sm">{t("stream")}</span>
            <Switch checked={stream} onChange={setStream} label={t("stream")} />
          </div>

          <div className="space-y-2 rounded-md border border-slate-200 p-3 dark:border-slate-800">
            <div className="flex items-center justify-between">
              <span className="text-sm">{t("enableThinking")}</span>
              <Switch checked={thinking} onChange={setThinking} label={t("enableThinking")} />
            </div>
            {thinking ? (
              <Field label={t("reasoningEffort")}>
                <Select value={effort} onChange={(event) => setEffort(event.target.value)}>
                  <option value="low">low</option>
                  <option value="medium">medium</option>
                  <option value="high">high</option>
                </Select>
              </Field>
            ) : null}
            <p className="text-xs text-amber-700 dark:text-amber-300">{t("thinkingHint")}</p>
          </div>
        </div>

        <div className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium">{t("messages")}</span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setMessages((prev) => [...prev, { role: "user", content: "" }])}
            >
              <Plus className="h-3.5 w-3.5" />
              {t("addMessage")}
            </Button>
          </div>

          <div className="space-y-2">
            {messages.map((message, index) => (
              <div key={index} className="flex items-start gap-2">
                <Select
                  className="w-28 shrink-0"
                  value={message.role}
                  aria-label={t("role")}
                  onChange={(event) => updateMessage(index, { role: event.target.value as Role })}
                >
                  <option value="system">system</option>
                  <option value="user">user</option>
                  <option value="assistant">assistant</option>
                </Select>
                <Textarea
                  rows={2}
                  className="flex-1"
                  value={message.content}
                  placeholder={t("content")}
                  onChange={(event) => updateMessage(index, { content: event.target.value })}
                />
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label={tc("delete")}
                  disabled={messages.length <= 1}
                  onClick={() => setMessages((prev) => prev.filter((_, i) => i !== index))}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
          </div>

          <div className="flex items-center gap-2">
            <Button disabled={running} onClick={() => void send()}>
              <Send className="h-3.5 w-3.5" />
              {running ? t("sending") : t("send")}
            </Button>
            <Button variant="ghost" onClick={clear}>
              {t("clear")}
            </Button>
          </div>
        </div>
      </div>

      {failure ? (
        <div className="rounded-lg border border-rose-300 bg-rose-50 p-4 text-sm dark:border-rose-900 dark:bg-rose-950/40">
          <div className="flex items-center gap-2 font-medium text-rose-800 dark:text-rose-200">
            <AlertTriangle className="h-4 w-4" />
            {t("requestFailed", { status: failure.status })}
          </div>
          <div className="mt-2 whitespace-pre-wrap break-words font-mono text-xs text-rose-800 dark:text-rose-200">
            {failure.message}
          </div>
          {failure.status === 400 ? (
            <div className="mt-2 rounded-md border border-amber-300 bg-amber-50 p-2 text-xs text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200">
              {t("badRequestHint")}
            </div>
          ) : null}
          {failure.raw && failure.raw.trim() !== failure.message ? (
            <details className="mt-2">
              <summary className="cursor-pointer text-xs text-rose-700 dark:text-rose-300">
                {t("rawBody")}
              </summary>
              <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded bg-white p-2 font-mono text-xs dark:bg-slate-950">
                {failure.raw}
              </pre>
            </details>
          ) : null}
        </div>
      ) : null}

      <div className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-sm font-medium">{t("output")}</span>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <span className="text-slate-500 dark:text-slate-400">
              {t("ttft")}:{" "}
              <span className="font-mono">
                {ttft === null ? "—" : `${Math.round(ttft)} ms`}
              </span>
            </span>
            <span className="text-slate-500 dark:text-slate-400">
              {t("servedBy")}: <span className="font-mono">{servedBy || "—"}</span>
            </span>
          </div>
        </div>

        {reasoning ? (
          <div>
            <div className="text-xs text-slate-500 dark:text-slate-400">{t("reasoning")}</div>
            <pre className="mt-1 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 p-2 font-mono text-xs dark:border-slate-800 dark:bg-slate-950">
              {reasoning}
            </pre>
          </div>
        ) : null}

        <pre
          className={cn(
            "min-h-32 max-h-96 overflow-auto whitespace-pre-wrap break-words rounded-md border border-slate-200 bg-slate-50 p-3 font-mono text-xs dark:border-slate-800 dark:bg-slate-950",
            running && "animate-pulse",
          )}
        >
          {output || t("outputPlaceholder")}
        </pre>

        <div>
          <div className="text-xs text-slate-500 dark:text-slate-400">{t("usage")}</div>
          <div className="mt-1 flex flex-wrap gap-3 text-xs">
            <span>
              prompt: <span className="font-mono">{usage?.prompt_tokens ?? 0}</span>
            </span>
            <span>
              completion: <span className="font-mono">{usage?.completion_tokens ?? 0}</span>
            </span>
            <span>
              total: <span className="font-mono">{usage?.total_tokens ?? 0}</span>
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
