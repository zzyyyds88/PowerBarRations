# 拆分文档：全管理面统一响应信封 + 按名寻址

> 大任务拆分（AGENTS.md）。设计依据：`docs/design-v1.md` §5.1/§16.3、`docs/api-spec-v1.md` §2/§3/§5.3.x/§9。
> 完成后本文件按流程删除（`chore:`），过程只留 Git 历史。

## 机制（已定）

1. **信封单一来源**：`internal/apiresp` 定义成功/失败两种写法的唯一实现；`common.ApiSuccess`、`common.ApiError`、
   `ApiErrorMsg`、`ApiErrorI18n` 改为委托它（一次性覆盖 ~139 个调用点）。
2. **逐路由响应策略表**：`internal/api/ops_routes.go`（稳定面 45 条，含成功体与失败映射）与
   `internal/apiresp` 的 `METHOD + c.FullPath()` 注册表（基座面需要自定义成功体的路由）。
   守卫测试强制：已注册路由必须有策略。
3. **裸 `{success:...}` 站点**：controller 内约 60 处手写信封改为 `apiresp` 写法。
4. **SSE 直通**：`text/event-stream` 不套信封。

## 波次

| 波 | 内容 | 验收 |
|---|---|---|
| W0 | 设计文档（已提交 `7fcfb76`） | docs 单独提交 |
| W1 | `internal/apiresp` + `common` 委托 + `apierr` details/新码 + 守卫测试骨架 | `go build`；新包单测 |
| W2 | 稳定面 45 条策略 + 按名寻址（`channels`/`models`）+ copy/fetch-models 修复 + 角色门移除 | 稳定端点单测 |
| W3 | 基座面：60 处裸信封 + 12 个 controller 失败分支显式化 + 逐路由策略登记 | 全量 Go 测试 |
| W4 | 前端 18 文件 + 类型 + 23 测试文件迁移 | `pnpm typecheck/lint/test/build` |
| W5 | OpenAPI 描述同步 + 真机冒烟 | 5700 实测 |
| W6 | 删除本拆分文档 + 删除 `/root/pbr-handoff.md` | 工作区干净 |

## 文件清单

- 新增：`internal/apiresp/apiresp.go`、`internal/apiresp/policies.go`、`internal/api/ops_routes.go`、`internal/apiresp/apiresp_test.go`、`internal/api/ops_routes_test.go`
- 修改：`common/gin.go`、`internal/apierr/apierr.go`、`internal/api/{ops,model_metadata,config_lifecycle}.go`、`router/{pbr-router,api-router,channel-router}.go`、`middleware/{pbr_auth,audit}.go`、`controller/*.go`（12 文件）、`web/src/**`

## 风险

- 一次性改 139 个 `common.Api*` 调用点：靠编译器 + 全量测试兜底。
- 前端 101 调用点：类型先行，编译器驱动找漏改。
- 失败状态码变化会改变控制台错误分支：同批提交，前后端一起绿。
