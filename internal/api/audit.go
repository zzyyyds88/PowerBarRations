package api

import (
	"net/http"
	"strconv"

	"pbr/model"

	"github.com/gin-gonic/gin"
)

// 管理面审计（api-spec §2.6）。W3 阶段管理面只有一个身份，actor 固定为 admin；
// 只记元数据与对象摘要，不记密钥明文与请求正文。

const pbrAuditActor = "admin"

// writeAudit 记录一次变更。
//
// 一个 payload 时记为 after_digest；两个 payload 时分别记为 before/after，
// 便于事后还原"改了什么"（api-spec §2.6）。
func writeAudit(c *gin.Context, action, resource, name string, payload ...any) {
	var before, after string
	switch len(payload) {
	case 1:
		after = model.DigestOf(payload[0])
	case 2:
		before = model.DigestOf(payload[0])
		after = model.DigestOf(payload[1])
	}
	_ = model.WritePBRAudit(pbrAuditActor, action, resource, name, before, after, dryRun(c))
}

// ListAudit GET /api/v1/audit
func ListAudit(c *gin.Context) {
	limit, cursor, err := pageParams(c)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	beforeId := 0
	if cursor != "" {
		beforeId, _ = strconv.Atoi(cursor)
	}
	entries, err := model.ListPBRAudits(limit, beforeId)
	if err != nil {
		writeAPIError(c, err)
		return
	}
	var nextCursor any
	if len(entries) > limit {
		nextCursor = encodeCursor(strconv.Itoa(entries[limit-1].Id))
		entries = entries[:limit]
	}
	items := make([]gin.H, 0, len(entries))
	for _, entry := range entries {
		items = append(items, gin.H{
			"id": entry.Id,
			// api-spec §2.5：对外时间一律 RFC3339（此前直接透传 Unix 秒，
			// 与 /logs 的 ts 表示不一致，调用方按 RFC3339 解析会得到错误年份）。
			"ts":            rfc3339(entry.Ts),
			"actor":         entry.Actor,
			"action":        entry.Action,
			"resource":      entry.Resource,
			"name":          entry.Name,
			"before_digest": entry.BeforeDigest,
			"after_digest":  entry.AfterDigest,
			"dry_run":       entry.DryRun,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": nextCursor})
}
