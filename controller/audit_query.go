package controller

import (
	"strconv"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
)

// GetAuditLogs GET /api/audit —— 基座审计日志（PBR 管理密钥鉴权）。
//
// W7（design-v1 §10.2.1）：多用户面已物理删除，因此这里不再有"用户自助视图"
// （原 `/api/audit/self`）与按角色收敛的分支；个人访问令牌（PAT）也一并删除，
// 但 `token_ref` 过滤保留，用于查既有审计数据。管理密钥是全量权限
// （token-spec §2），故按 root 视角查询。
func GetAuditLogs(c *gin.Context) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page > 100000000 {
		common.ApiErrorMsg(c, "Invalid audit pagination")
		return
	}
	filter := model.AuditLogFilter{
		Username:        c.Query("username"),
		Category:        c.Query("category"),
		TokenRef:        c.Query("token_ref"),
		ExcludeTokenRef: c.Query("exclude_token_ref"),
		RequestId:       c.Query("request_id"),
	}
	if !model.ValidAuditCategory(filter.Category) || !model.ValidTokenFingerprint(filter.TokenRef) || !model.ValidTokenFingerprint(filter.ExcludeTokenRef) {
		common.ApiErrorMsg(c, "Invalid audit filters")
		return
	}
	for name, target := range map[string]*int64{"start_timestamp": &filter.StartTimestamp, "end_timestamp": &filter.EndTimestamp} {
		if raw := c.Query(name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				common.ApiErrorMsg(c, "Invalid audit time range")
				return
			}
			*target = parsed
		}
	}
	if filter.EndTimestamp > 0 && filter.EndTimestamp < filter.StartTimestamp {
		common.ApiErrorMsg(c, "Invalid audit time range")
		return
	}
	if raw := c.Query("success"); raw != "" {
		if raw != "true" && raw != "false" {
			common.ApiErrorMsg(c, "Invalid audit result")
			return
		}
		success := raw == "true"
		filter.Success = &success
	}
	logs, total, err := model.GetAuditLogs(filter, page.GetStartIdx(), page.GetPageSize(), common.RoleRootUser)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetItems(logs)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}
