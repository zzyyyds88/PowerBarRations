# L2 真实 API 运维（`docs/test-spec-v1.md` §5）

只用管理 API（`Authorization: Bearer <管理密钥>`，由登录口令派生）模拟运维人员一天的活：
探活 → 建渠道 → 建车道 → 端到端调用 → 排障 → 逐成员探活 → 故障注入与转移 →
重置熔断 → 密钥轮换 → 导出/导入幂等 → 日志保留 → 审计。每步"调用 → 回读 → 断言"。

## 复现

```bash
bash verify/e2e-api/ops_runbook.sh
```

全程本地：独立端口 6841 + 独立 SQLite + 内置假上游（6842）；不打真实厂商、不碰现网。
日志写 `verify/e2e-api/run-<时间戳>.log`（不入库）。

## 最近一次实测

| 时间 | 结果 | 证据 |
|---|---|---|
| 2026-09-19 | **PASS=21 FAIL=0** | `verify/e2e-api/run-20260919-130927.log` |
| 2026-09-19 | **PASS=21 FAIL=0** | `verify/e2e-api/run-20260919-105901.log` |
