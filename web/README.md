# PowerBarRations 控制台

个人自用网关的浏览器界面。以线上路由层前端（`reference/octopus-bestrui/web`）的**功能原理**
为蓝本重写，不是照搬其代码：保留"车道成员拖拽排序 = 写 priority"、"运行态实时徽标"、
"空/加载/错三态齐备"这几条机制，API 全部换成 PBR 的管理面契约。

## 技术栈与包管理

Vite + React 19 + TypeScript + Tailwind 4；包管理沿用 **pnpm**（`pnpm-lock.yaml` 入库）。

```bash
cd web
pnpm install
pnpm dev        # 开发态，/api 与 /v1 自动代理到 127.0.0.1:5700
pnpm build      # tsc --noEmit && vite build → dist/
```

## 产物形态

- `pnpm build` 产出 `web/dist/`，由根目录 `main.go` 的 `go:embed web/dist` 打进二进制，
  单进程同时服务 `/v1/*`、`/api/v1/*` 与静态控制台。
- 仓库只跟踪 `web/dist/index.html` 这一个**占位页**（保证未构建控制台时 `go build` 仍可用）；
  `web/dist/assets/` 是构建产物，不入库。
- Docker 构建会在 node 阶段先 `pnpm build`，再把它拷进 Go 构建阶段。

## 页面与实时机制

| 页面 | 说明 |
|---|---|
| 仪表盘 | 可路由模型数、近期成功率、熔断参数概览 |
| 车道 | 成员**拖拽排序**（写回 priority）+ 冷却/熔断/半开**实时徽标** + 带时间戳的运行态事件 |
| 渠道 | 渠道增删、模型清单声明、单渠道探活；密钥只写不读 |
| 模型路由 | 全部可路由模型与其成员链；显式车道可逐成员探活 |
| 请求日志 | 元数据日志 + attempts 链展开（为什么没用 P1、为什么 503） |
| 客户端密钥 | 创建/轮换（明文只显示一次）、启停、显式拒绝车道 |
| 设置 | 熔断阈值/打开时长/退避上限、欠费关键词表、导出配置 |

实时机制（routing-spec §7）：**SSE（fetch + ReadableStream）加速 + 30s 轮询兜底**。
不用 `EventSource`——它无法带 `Authorization` 头，而管理密钥只走 Bearer。

## 许可

控制台源码由本项目自行编写（未复制 octopus 前端代码），与仓库其余部分同为 AGPL；
后端基座来自 new-api，版权头与 `LICENSE`/`NOTICE`/`THIRD-PARTY-LICENSES.md` 均保留在仓库根。
