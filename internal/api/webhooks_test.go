package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/internal/apierr"
	"github.com/zzyyyds88/PowerBarRations/internal/webhook"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWebhookTestDB 提供 options 表与投递日志表的测试库（prune 用例还需明细日志表）。
func setupWebhookTestDB(t *testing.T) {
	t.Helper()
	setupImportTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.WebhookDelivery{}, &model.PBRRequestLog{}))
}

func webhookRequest(t *testing.T, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	if body != "" {
		c.Request.Header.Set("Content-Type", "application/json")
	}
	return c, recorder
}

// 配置 PUT/GET 回读 + secret 掩码 + 空 secret 保留原值（api-spec §5.8）。
func TestWebhookTargetsPutGetRoundTrip(t *testing.T) {
	setupWebhookTestDB(t)

	// PUT 一条带完整 secret 的 target。
	c, rec := webhookRequest(t, http.MethodPut, "/api/webhooks",
		`{"targets":[{"name":"notify","url":"http://127.0.0.1:8645/webhooks/xxx",
		  "secret":"super-long-secret-1234","enabled":true,"events":["circuit_open"]}]}`)
	PutWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var putBody struct {
		Targets []struct {
			Name    string   `json:"name"`
			URL     string   `json:"url"`
			Secret  string   `json:"secret"`
			Enabled bool     `json:"enabled"`
			Events  []string `json:"events"`
		} `json:"targets"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &putBody))
	require.Len(t, putBody.Targets, 1)
	assert.Equal(t, "****1234", putBody.Targets[0].Secret, "secret 必须只回显掩码")
	assert.Equal(t, "http://127.0.0.1:8645/webhooks/xxx", putBody.Targets[0].URL)
	assert.True(t, putBody.Targets[0].Enabled)
	assert.Equal(t, []string{"circuit_open"}, putBody.Targets[0].Events)

	// GET 回读同一形状（secret 掩码）。
	c, rec = webhookRequest(t, http.MethodGet, "/api/webhooks", "")
	ListWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code)
	var getBody struct {
		Targets json.RawMessage `json:"targets"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &getBody))
	var targets []map[string]any
	require.NoError(t, json.Unmarshal(getBody.Targets, &targets))
	require.Len(t, targets, 1)
	assert.Equal(t, "****1234", targets[0]["secret"])
	assert.Equal(t, "notify", targets[0]["name"])

	// secret 留空 = 保留原值：明文仍在 options 里，回显仍是掩码。
	c, rec = webhookRequest(t, http.MethodPut, "/api/webhooks",
		`{"targets":[{"name":"notify","url":"http://127.0.0.1:8645/webhooks/xxx","enabled":true,"events":[]}]}`)
	PutWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	raw, ok := common.OptionMap[webhook.OptionKey]
	require.True(t, ok, "配置必须已写入 options 内存表")
	assert.Contains(t, raw, "super-long-secret-1234", "空 secret 必须保留原值")

	// 重新给显式 secret 会覆盖原值。
	c, rec = webhookRequest(t, http.MethodPut, "/api/webhooks",
		`{"targets":[{"name":"notify","url":"http://127.0.0.1:8645/webhooks/xxx","secret":"newsecret-9876","enabled":true,"events":[]}]}`)
	PutWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	raw, ok = common.OptionMap[webhook.OptionKey]
	require.True(t, ok)
	assert.Contains(t, raw, "newsecret-9876")
	assert.NotContains(t, raw, "super-long-secret-1234")

	// 短 secret 全掩码。
	c, rec = webhookRequest(t, http.MethodPut, "/api/webhooks",
		`{"targets":[{"name":"notify","url":"http://127.0.0.1:8645/webhooks/xxx","secret":"abc","enabled":true,"events":[]}]}`)
	PutWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var shortBody struct {
		Targets []struct {
			Secret string `json:"secret"`
		} `json:"targets"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &shortBody))
	require.Len(t, shortBody.Targets, 1)
	assert.Equal(t, "****", shortBody.Targets[0].Secret, "不足 8 字符必须全掩码")
}

// 配置校验：重复 name / 非 http(s) url / 非法 events / 缺 targets 字段都必须 400。
func TestWebhookTargetsPutValidation(t *testing.T) {
	setupWebhookTestDB(t)

	cases := []struct {
		name string
		body string
	}{
		{"重复 name", `{"targets":[{"name":"a","url":"http://a.example/x"},{"name":"a","url":"http://b.example/x"}]}`},
		{"空 name", `{"targets":[{"name":"","url":"http://a.example/x"}]}`},
		{"非 http(s) url", `{"targets":[{"name":"a","url":"ftp://a.example/x"}]}`},
		{"无 scheme", `{"targets":[{"name":"a","url":"a.example/x"}]}`},
		{"非法 events", `{"targets":[{"name":"a","url":"http://a.example/x","events":["bogus"]}]}`},
		{"缺 targets", `{}`},
	}
	for _, tc := range cases {
		c, rec := webhookRequest(t, http.MethodPut, "/api/webhooks", tc.body)
		PutWebhooks(c)
		require.Equal(t, http.StatusBadRequest, rec.Code, tc.name+": "+rec.Body.String())
		var errBody struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
		assert.Equal(t, apierr.CodeValidationFailed, errBody.Error.Code, tc.name)
	}

	// 显式空数组 = 清空全部目标（合法）。
	c, rec := webhookRequest(t, http.MethodPut, "/api/webhooks", `{"targets":[]}`)
	PutWebhooks(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"targets":[]}`, rec.Body.String())
}

// test 端点同步语义：同步等待最终结果，响应即投递结果；未知 name 404。
func TestWebhookTestEndpointSyncSemantics(t *testing.T) {
	setupWebhookTestDB(t)

	var mu sync.Mutex
	var requests int
	var headers http.Header
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		body, _ = io.ReadAll(r.Body)
		mu.Lock()
		requests++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	targetsJSON, err := json.Marshal([]webhook.Target{
		{Name: "notify", URL: server.URL, Secret: "secret-abcdef", Enabled: true},
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateOption(webhook.OptionKey, string(targetsJSON)))

	// 成功路径：status success、attempts=1、http_status=200。
	c, rec := webhookRequest(t, http.MethodPost, "/api/webhooks/test", `{"name":"notify"}`)
	TestWebhook(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"success","http_status":200,"error":"","attempts":1}`, rec.Body.String())
	mu.Lock()
	assert.Equal(t, 1, requests)
	mu.Unlock()

	// 请求头与请求体符合 /doc §5.2/§5.3：签名头、application/json、type=pbr。
	assert.Equal(t, "application/json", headers.Get("Content-Type"))
	assert.NotEmpty(t, headers.Get("X-Webhook-Timestamp"))
	assert.NotEmpty(t, headers.Get("X-Webhook-Signature-V2"))
	assert.Contains(t, string(body), `"type":"pbr"`)
	assert.Contains(t, string(body), "测试事件")

	// 未知 name：404 + 稳定错误码。
	c, rec = webhookRequest(t, http.MethodPost, "/api/webhooks/test", `{"name":"ghost"}`)
	TestWebhook(c)
	require.Equal(t, http.StatusNotFound, rec.Code)
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errBody))
	assert.Equal(t, apierr.CodeWebhookTargetNotFound, errBody.Error.Code)

	// 缺 name：400。
	c, rec = webhookRequest(t, http.MethodPost, "/api/webhooks/test", `{}`)
	TestWebhook(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// deliveries 分页（cursor / limit 语义与 /api/logs 一致，耗尽时游标为 null）。
func TestListWebhookDeliveriesPagination(t *testing.T) {
	setupWebhookTestDB(t)

	for i := 0; i < 5; i++ {
		require.NoError(t, model.InsertWebhookDelivery(&model.WebhookDelivery{
			Ts: int64(1789600000 + i), Target: "notify", EventType: "circuit_open",
			Status: "success", HTTPStatus: 200, Attempt: 1,
		}))
	}

	// 第一页 limit=2。
	c, rec := webhookRequest(t, http.MethodGet, "/api/webhooks/deliveries?limit=2", "")
	ListWebhookDeliveries(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)
	item := page.Items[0]
	assert.Equal(t, "notify", item["target"])
	assert.Equal(t, "circuit_open", item["event_type"])
	assert.Equal(t, "success", item["status"])
	assert.Contains(t, item, "ts")
	assert.Contains(t, item, "lane")
	assert.Contains(t, item, "member")
	assert.Contains(t, item, "http_status")
	assert.Contains(t, item, "error")
	assert.Contains(t, item, "attempt")

	// 第二页：游标翻页。
	c, rec = webhookRequest(t, http.MethodGet, "/api/webhooks/deliveries?limit=2&cursor="+*page.NextCursor, "")
	ListWebhookDeliveries(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.NotNil(t, page.NextCursor)

	// 最后一页：1 条 + null 游标。
	c, rec = webhookRequest(t, http.MethodGet, "/api/webhooks/deliveries?limit=2&cursor="+*page.NextCursor, "")
	ListWebhookDeliveries(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Nil(t, page.NextCursor, "耗尽时 next_cursor 必须是 null")

	// 非法 cursor 与非法 limit 必须报 400，不得静默回第一页。
	c, rec = webhookRequest(t, http.MethodGet, "/api/webhooks/deliveries?cursor=!!!bad!!!", "")
	ListWebhookDeliveries(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	c, rec = webhookRequest(t, http.MethodGet, "/api/webhooks/deliveries?limit=0", "")
	ListWebhookDeliveries(c)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// webhook_deliveries 随 POST /api/logs/prune 按 retention 同步清理（design-v1 §16.10）。
func TestPruneCleansWebhookDeliveries(t *testing.T) {
	setupWebhookTestDB(t)

	old := int64(1000)
	recent := int64(1789600000)
	require.NoError(t, model.InsertWebhookDelivery(&model.WebhookDelivery{
		Ts: old, Target: "notify", EventType: "circuit_open", Status: "failed", Attempt: 4}))
	require.NoError(t, model.InsertWebhookDelivery(&model.WebhookDelivery{
		Ts: recent, Target: "notify", EventType: "cooldown", Status: "success", Attempt: 1}))

	c, rec := webhookRequest(t, http.MethodPost, "/api/logs/prune?before=1789500000", "")
	PruneLogs(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		WebhookDeleted int64 `json:"webhook_deliveries_deleted"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.EqualValues(t, 1, body.WebhookDeleted)

	entries, err := model.ListWebhookDeliveries(10, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.EqualValues(t, recent, entries[0].Ts, "保留期内的投递记录不得被清")
}
