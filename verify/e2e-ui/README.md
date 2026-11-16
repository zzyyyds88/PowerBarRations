# L3 真实用户操作（`docs/test-spec-v1.md` §4）

用无头 Chromium + CDP 像真人一样操作控制台，每一步由 UI 触发，再由后端回读佐证：

1. 首启设口令（`/setup` 表单）
2. 登出/登录（`/sign-in`，含错误口令被拒）
3. 新建渠道（渠道管理 → Create Channel 表单）
4. 新建路由车道（路由页卡片 → Create lane / New lane，两栏编排器）
5. 新建客户端密钥（令牌页 → Create API Key 抽屉）
6. 试打台对话
7. 查看请求日志（含客户端/会话/上下文列与上下文卡片）
8. 查看会话列表与单会话时间线（上下文曲线）
9. 概览用量洞察（缓存命中率/客户端分布/53 周热力图，且不再渲染实时路由卡）
10. 实时路由单页（拓扑、运行态、时间区间/车道深链；同页渠道速度面板：5m/15m/1h、渠道整体输出速度、实时流量、请求速率、空闲条带）
11. 脱敏页开关/预览/命中记录
12. 分组侧边栏与设置下拉（设置原地展开；进入设置域后主侧边栏常驻）
13. 系统设置回读

## 复现

```bash
python3 verify/e2e-ui/user_journey.py
```

自包含：自行 `pnpm build` + `go build`、拉独立端口 + 独立 SQLite + 内置假上游 + 无头 Chromium。
日志写 `verify/e2e-ui/run-<时间戳>.log`，截图写 `verify/e2e-ui/shots/`（均不入库）。
缺 chromium 或 python `websocket-client` 时 SKIP（退出码 2）。

## 最近一次实测

| 时间 | 结果 | 证据 |
|---|---|---|
| 2026-11-15（实时路由单页：拓扑 + 渠道整体输出速度 + 固定 15m 实时流量；输出速度=窗口 completion_tokens/窗口秒数；单位 tok/s） | **PASS=112 FAIL=0** | `run-20261115-140235.log` |
| 2026-11-15（实时路由单页：概览移除拓扑、同页车道拓扑与渠道速度、速度 API 干净样本、短窗 URL、不再有独立仪表盘入口；侧边栏顺序为实时路由/路由/渠道与模型/API 密钥/用量/脱敏） | **PASS=110 FAIL=0** | `run-20261115-101514.log` |
| 2026-11-15（分组侧边栏 + 设置下拉：用量四子项、渠道与模型/脱敏入口、设置原地展开、设置页主侧边栏常驻与域内分节） | **PASS=102 FAIL=0** | `run-20261115-023804.log` |
| 2026-11-14（用量元数据 + 上下文页 + 隐私脱敏页：日志客户端/会话/上下文、会话页、上下文页、概览洞察、脱敏开关/预览/记录） | **PASS=94 FAIL=0** | `run-20261114-164322.log` |
| 2026-11-07（UI 四项：渠道降噪/日志双密度/健康条/模型价格健康列） | **PASS=68 FAIL=0** | `run-20261107-070552.log`；Playwright `playwright/run-20261107-070533.log` 1 passed |
| 2026-11-06（基线 main） | **PASS=58 FAIL=10** | `run-20261106-135720.log`；日志工作区 404 |
| 2026-11-07（债务修复后） | **PASS=68 FAIL=0** | `run-20261107-042823.log` |
| 2026-11-06（全量升级） | **PASS=59 FAIL=9** | `run-20261106-182500.log`；日志页 PASS，其余 9 项与基线逐条相同 |
| 2026-09-19 | **PASS=29 FAIL=0** | `verify/e2e-ui/run-20260919-131458.log` |
| 2026-09-19 | **PASS=27 FAIL=0** | `verify/e2e-ui/run-20260919-*.log` |

**基线失败已修复（2026-11-07）**：升级过程中发现的 9 项导航时序/渠道状态选择器失败
已通过把 URL 断言改成等待、以及把详情抽屉入口切到渠道页行菜单「Status」修复；
最近一次 **PASS=68 FAIL=0**（`run-20261107-042823.log`）。

L3-E2E（Playwright）另见 `verify/e2e-ui/playwright/run.sh`，覆盖登录、7+1 路由、日志三页签、
命令面板、导出入口，以及渠道行动作降噪、日志窗口 KPI / 表格-卡片切换与 localStorage 记忆、
模型目录价格/健康列头；最近一次 **1 passed / PASS**。

## 手动操作（真实图形浏览器，人可亲手点）

自动化脚本之外，另提供**可见的真实浏览器环境**供人手动操作（不是 CDP 脚本驱动）：

```bash
bash verify/e2e-ui/live_browser.sh start    # 起 Xvfb + openbox + 有头 Chromium + noVNC
bash verify/e2e-ui/live_browser.sh status   # 查看状态与访问地址
bash verify/e2e-ui/live_browser.sh shot 名  # 抓当前屏幕截图
bash verify/e2e-ui/live_browser.sh stop     # 停止
```

- 浏览器**真实可见**（Xvfb 虚拟显示 :99 + openbox），经 x11vnc + noVNC 暴露为网页：
  `http://<本机IP>:6080/vnc.html?autoconnect=1&resize=scale`，在浏览器里用**真实鼠标键盘**操作。
- 已预置演示数据（渠道 demo-channel / 车道 demo-model / 客户端密钥），可直接点。
- 可用 `xdotool` 在 `DISPLAY=:99` 上注入 OS 级鼠标/键盘事件（等价真人输入）。
- 依赖：`Xvfb x11vnc websockify novnc openbox xdotool imagemagick`。

## 覆盖的用户路径

首启设口令、登出/登录（含错误口令被拒）、新建渠道（表单）、新建/编辑车道成员链（卡片 + 两栏编排器，ADR 0006）、
新建客户端密钥（抽屉）、试打台对话（含展示 `X-Served-By`）、查看请求日志（客户端/会话/上下文）、
会话列表与单会话时间线、概览用量洞察、实时路由单页（拓扑 + 同页各渠道输出速度、实时流量与窗口切换）、
脱敏页开关/预览/命中记录、改系统设置并回读。

> 历史：试打台展示 `X-Served-By`（ui-spec §6.8）曾缺失，L3 以非计分 NOTE 记录；
> 该缺口已在 `feat(web): 试打台展示 X-Served-By 响应头` 补齐，现为计分断言。
