# L3 真实用户操作（`docs/test-spec-v1.md` §4）

用无头 Chromium + CDP 像真人一样操作控制台，每一步由 UI 触发，再由后端回读佐证：

1. 首启设口令（`/setup` 表单）
2. 登出/登录（`/sign-in`，含错误口令被拒）
3. 新建渠道（渠道管理 → Create Channel 表单）
4. 新建路由车道（路由页 → New lane）
5. 新建客户端密钥（令牌页 → Create API Key 抽屉）
6. 试打台对话
7. 查看请求日志
8. 系统设置回读

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
| 2026-09-19 | **PASS=27 FAIL=0** | `verify/e2e-ui/run-20260919-*.log` |

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

首启设口令、登出/登录（含错误口令被拒）、新建渠道（表单）、编辑车道成员链、
新建客户端密钥（抽屉）、试打台对话（含展示 `X-Served-By`）、查看请求日志、改系统设置并回读。

> 历史：试打台展示 `X-Served-By`（ui-spec §6.8）曾缺失，L3 以非计分 NOTE 记录；
> 该缺口已在 `feat(web): 试打台展示 X-Served-By 响应头` 补齐，现为计分断言。
