package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 列表端点的分页契约（api-spec §2.4）：耗尽/不分页时 next_cursor 必须是 null。
//
// 回归背景：/models 曾硬编码空串，而其余列表端点用 nil 变量输出 null。
// 空串会让严格判空 `next_cursor === null` 的客户端把它当成有效游标，
// 从而反复请求第一页。
func TestListEndpointsUseNullCursorWhenExhausted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	t.Cleanup(func() { model.DB = originalDB })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Lane{}, &model.LaneMember{}, &model.ClientKey{}))

	// 空库：所有列表端点都应给出 null 游标
	cases := []struct {
		name    string
		handler gin.HandlerFunc
	}{
		{"models", ListModels},
		{"lanes", ListLanes},
		{"keys", ListKeys},
		{"channels", ListChannels},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(rec)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)

		tc.handler(ctx)

		var body struct {
			NextCursor *string `json:"next_cursor"`
		}
		require.NoErrorf(t, json.Unmarshal(rec.Body.Bytes(), &body),
			"%s 响应不是合法 JSON: %s", tc.name, rec.Body.String())
		if body.NextCursor != nil {
			t.Errorf("%s: 耗尽时 next_cursor 应为 null，实际为 %q（空串会被当成有效游标）",
				tc.name, *body.NextCursor)
		}
	}
}
