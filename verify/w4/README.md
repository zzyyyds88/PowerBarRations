# W4 验收证据：控制台

## 目标（goal-prompt §四 W4 / design-v1 §6、ui-spec §8）

以线上路由层前端（`reference/octopus-bestrui/web`）的**功能原理**为蓝本迁移控制台，
包管理沿用 pnpm；品牌全量换 PowerBarRations；SSE 用 fetch+ReadableStream（不用 EventSource）
+30s 轮询兜底；保留拖拽排序与实时状态徽标。门是 `pnpm build` 零报错 + ui-spec §8 验收总纲全过。

## 做了什么

| 层 | 文件 | 内容 |
|---|---|---|
| 后端 | `internal/api/route_events.go` | `GET /api/v1/route-events`：SSE 推送车道运行态（当前成员/探测占用/亲和/冷却表 + 带时间戳事件），连接即推全量快照 |
| 工程 | `web/package.json`、`vite.config.ts`、`tsconfig.json`、`postcss.config.mjs` | Vite + React 19 + TS + Tailwind 4，pnpm 锁文件入库；产物 `web/dist` 由 `go:embed` 打进二进制 |
| 外壳 | `web/src/App.tsx`、`api.ts`、`components/AsyncState.tsx` | 七个页签；统一错误包络解析；**空/加载/错三态组件** |
| 实时 | `web/src/hooks/useRouteEvents.ts` | **fetch + ReadableStream 消费 SSE**（EventSource 无法带 Authorization 头）+ **30s 轮询兜底** |
| 页面 | `web/src/pages/*.tsx` | 仪表盘、车道（**拖拽排序 → 写回 priority** + 冷却/熔断/半开**实时徽标** + 带时间戳事件）、渠道（含单渠道探活）、模型路由（成员链 + 逐成员探活）、请求日志（**attempts 链展开**）、客户端密钥（创建/轮换明文只显示一次、显式拒绝车道）、设置（熔断参数/欠费关键词/导出配置）、登录（首启设口令） |
| 构建链 | `Dockerfile` | node 阶段先 `pnpm build`，产物拷进 Go 阶段供 embed |

蓝本是 85 个源文件的完整应用；按设计"迁移功能原理、不照搬巨型控制台"的要求，控制台为
本项目自写（未复制其代码），保留其机制：拖拽写 priority、SSE 实时状态、轮询兜底、三态齐备。

## 复现

```bash
bash verify/w4/smoke.sh        # 构建控制台 → embed 进二进制 → 起服务 → 校验静态资源与 SSE
```

## 结果（run-20260915-032336.log，PASS=19 FAIL=0）

| 检查项 | 结论 | 证据 |
|---|---|---|
| `pnpm build` 零报错 | `tsc --noEmit && vite build` 退出码 0，产出 hash 化 JS/CSS | 日志第 14–15 行 |
| 产物进二进制并由同一进程服务 | 首页引用 `/assets/index-*.js`，不再是 W0 占位页；入口 JS 可访问、内容类型为 javascript、含品牌与页面文案 | 日志第 34–40 行 |
| SPA 回退 | 前端路径回退到首页；未知 `/api/v1/*` 仍 404（不被首页吞掉） | 日志第 42–43 行 |
| SSE 可流 | `event: route-state`，载荷含 `lanes` 与成员 `circuit` 状态 | 日志第 50–52 行 |
| SSE 需鉴权 | 未带凭据返回 401 | 日志第 54 行 |
| 控制台 ↔ 契约一致 | 从控制台源码抽出的 **20 个管理端点**逐一在 `openapi.json` 中登记，`missing` 为空（含 `/route-events`、`/lanes/{name}/health`、`/channels/{name}/test`、`/keys/{name}/rotate` 等） | 日志第 57–58 行 |
| 构建产物不入库 | 验收结束还原 `web/dist/index.html` 占位页，工作区干净 | 日志第 60 行 |

回归：`go build ./...`、`go vet ./...`、`go test ./... -count=1` 全绿（41 个包）；
`verify/w1` PASS=22、`verify/w2` PASS=31、`verify/w3` PASS=44、`verify/w5` PASS=21 均未回退。

## 未验证项（明说，不计入通过）

**ui-spec §8 的页面级走查（视觉与交互）本机无法执行**：本环境的浏览器后端不可用
（`agent.browsers.list()` 返回空数组），没有可驱动的浏览器，因此下列项目**尚未实测**：

- 各页面的视觉呈现与布局；
- 车道页拖拽排序的实际交互（代码路径已实现，接口侧已由 W1/W2 验收覆盖 priority 语义）；
- SSE 徽标在真实故障时由 `closed → open → half_open` 的实时变化（接口侧已由 W2 的时间戳证据覆盖）。

需要补做时，在一台有浏览器的机器上按此清单走查并把截图/结论补进本文件：

1. 访问 `/` → 首启设口令页 → 用 ≥32 位随机串初始化 → 自动进入仪表盘（空态文案正确）。
2. 渠道页建渠道（协议/地址/模型清单/优先级）→ 列表出现 → 点「探活」看到成功与失败两种结果。
3. 模型路由页确认新模型自动出现在隐式链里（未建任何车道对象）。
4. 车道页建显式车道 → 拖拽交换两个成员 → 刷新后顺序保持（priority 已重排）。
5. 用坏 key 打一发请求：车道页成员徽标在数秒内变为「熔断打开」，运行态事件列表出现带时间戳的
   `circuit_open`；等待退避窗口后恢复请求 → 徽标回到「正常」，事件出现 `circuit_half_open`/`circuit_closed`。
6. 请求日志页展开失败那条 → attempts 链完整（失败段带 `error_kind`，末段 `success`）。
7. 客户端密钥页创建密钥 → 明文只显示一次；再读只有前缀；轮换后旧密钥 401。
8. 设置页改熔断阈值 → 保存后回读一致；导出配置下载成功且不含密钥明文。

## 本波未做（按波次划分/与设计的偏差）

- **Playground**（ui-spec 要求保留）：尚未实现；W8 的端到端金标准会覆盖四模式与流式，
  届时一并补页面。
- **三语 i18n**：当前界面为中文单语；蓝本用 `use-intl` 做三语。设计未把三语列为硬要求，
  但 ui-spec §1 提到"三语"，如需保留应补。
- **Recharts 图表**：仪表盘用数字卡片而非图表；W5 的 `/stats` 已具备数据源。
