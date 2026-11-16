package api

// 渠道价格峰谷校验（api-spec §4.1）：非法 HH:MM、空 days、days 越界、>64 bands、
// 非法 timezone、start == end、负单价 → 400 `invalid_prices`（统一 apierr 信封）。
// 校验必须发生在 dry-run 之前，控制台的批量粘贴预览（?dry_run=true）才能拦截。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func putChannelJSON(t *testing.T, name, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	return callAPI(t, http.MethodPut, "/api/v1/channels/"+name+query, body, PutChannel,
		gin.Params{{Key: "name", Value: name}})
}

func channelPriceBody(prices string) string {
	return `{"name":"ch-price","type":"openai","base_url":"https://upstream.example/v1",` +
		`"models":["model-a"],"prices":` + prices + `}`
}

// 合法峰谷：写后回读保留 timezone/bands 与显式 0，且首尾空白被规范化。
func TestPutChannelAcceptsPriceBands(t *testing.T) {
	setupAPITestDB(t)

	body := channelPriceBody(`[
		{"model":"model-a","input":1,"output":2,"timezone":" Asia/Shanghai ",
		 "bands":[
			{"days":[1,2,3,4,5],"start":" 09:00 ","end":"18:00","input":5},
			{"days":[6,7],"start":"22:00","end":"02:00","output":0}
		 ]}
	]`)
	recorder := putChannelJSON(t, "ch-price", "", body)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var response struct {
		Prices []struct {
			Model    string `json:"model"`
			Timezone string `json:"timezone"`
			Bands    []struct {
				Days   []int    `json:"days"`
				Start  string   `json:"start"`
				End    string   `json:"end"`
				Input  *float64 `json:"input"`
				Output *float64 `json:"output"`
			} `json:"bands"`
		} `json:"prices"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Len(t, response.Prices, 1)
	price := response.Prices[0]
	assert.Equal(t, "model-a", price.Model)
	assert.Equal(t, "Asia/Shanghai", price.Timezone, "timezone 去空白后落库并回读")
	require.Len(t, price.Bands, 2)
	assert.Equal(t, []int{1, 2, 3, 4, 5}, price.Bands[0].Days)
	assert.Equal(t, "09:00", price.Bands[0].Start, "start 去空白")
	require.NotNil(t, price.Bands[1].Output)
	assert.InDelta(t, 0, *price.Bands[1].Output, 1e-9, "显式 0 不能退化成未填")

	// 落库值与回读一致（写后回读：响应就是最终状态）。
	var stored model.Channel
	require.NoError(t, model.DB.Where("name = ?", "ch-price").First(&stored).Error)
	storedPrices := stored.GetSetting().PBRPrices
	require.Len(t, storedPrices, 1)
	assert.Equal(t, "Asia/Shanghai", storedPrices[0].Timezone)
	require.Len(t, storedPrices[0].Bands, 2)
}

func TestPutChannelRejectsInvalidPrices(t *testing.T) {
	tooManyBands := func() string {
		bands := make([]string, 0, 65)
		for i := 0; i < 65; i++ {
			bands = append(bands, `{"days":[1],"start":"09:00","end":"10:00"}`)
		}
		return `[{"model":"model-a","input":1,"bands":[` + strings.Join(bands, ",") + `]}]`
	}()

	tests := []struct {
		name   string
		prices string
		want   string
	}{
		{
			name:   "invalid HH:MM",
			prices: `[{"model":"model-a","bands":[{"days":[1],"start":"9:00","end":"18:00"}]}]`,
			want:   "must be HH:MM",
		},
		{
			name:   "invalid end HH:MM",
			prices: `[{"model":"model-a","bands":[{"days":[1],"start":"09:00","end":"24:00"}]}]`,
			want:   "must be HH:MM",
		},
		{
			name:   "empty days",
			prices: `[{"model":"model-a","bands":[{"days":[],"start":"09:00","end":"18:00"}]}]`,
			want:   "days must not be empty",
		},
		{
			name:   "days out of range",
			prices: `[{"model":"model-a","bands":[{"days":[0,8],"start":"09:00","end":"18:00"}]}]`,
			want:   "out of range 1-7",
		},
		{
			name:   "too many bands",
			prices: tooManyBands,
			want:   "bands must not exceed 64 entries",
		},
		{
			name:   "invalid timezone",
			prices: `[{"model":"model-a","timezone":"Mars/Olympus","bands":[{"days":[1],"start":"09:00","end":"18:00"}]}]`,
			want:   "timezone",
		},
		{
			name:   "empty range",
			prices: `[{"model":"model-a","bands":[{"days":[1],"start":"10:00","end":"10:00"}]}]`,
			want:   "start and end must differ",
		},
		{
			name:   "negative band price",
			prices: `[{"model":"model-a","bands":[{"days":[1],"start":"09:00","end":"18:00","input":-1}]}]`,
			want:   "must be >= 0",
		},
		{
			name:   "negative row price",
			prices: `[{"model":"model-a","input":-1}]`,
			want:   "unit price must be >= 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupAPITestDB(t)
			recorder := putChannelJSON(t, "ch-price", "", channelPriceBody(tt.prices))
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())

			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			assert.Equal(t, "invalid_prices", envelope.Error.Code)
			assert.Contains(t, envelope.Error.Message, tt.want)

			// 校验失败不得落库（创建路径整体回滚）。
			var count int64
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "ch-price").
				Count(&count).Error)
			assert.Zero(t, count)
		})
	}
}

// dry-run 预览（控制台批量粘贴导入）必须先跑价格校验。
func TestPutChannelDryRunValidatesPrices(t *testing.T) {
	setupAPITestDB(t)

	invalid := channelPriceBody(`[{"model":"model-a","bands":[{"days":[1],"start":"9:00","end":"18:00"}]}]`)
	recorder := putChannelJSON(t, "ch-price", "?dry_run=true", invalid)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "invalid_prices")

	valid := channelPriceBody(`[{"model":"model-a","input":1,"bands":[{"days":[1],"start":"09:00","end":"18:00"}]}]`)
	recorder = putChannelJSON(t, "ch-price", "?dry_run=true", valid)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"dry_run":true`)

	var count int64
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("name = ?", "ch-price").Count(&count).Error)
	assert.Zero(t, count, "dry-run 不落库")
}

// 日志响应必须带 price_band（-1=兜底），供控制台/导出展示（api-spec §4.4）。
func TestLogResponseIncludesPriceBand(t *testing.T) {
	entry := &model.PBRRequestLog{Id: 7, Ts: 1_700_000_000, PriceBand: 2}
	response := logResponse(entry)
	assert.Equal(t, 2, response["price_band"])

	entry.PriceBand = -1
	assert.Equal(t, -1, logResponse(entry)["price_band"], "兜底值必须原样暴露")
}

// 兼容性：旧单层价（无 timezone/bands）不受新增校验影响。
func TestPutChannelAcceptsLegacySingleLayerPrices(t *testing.T) {
	setupAPITestDB(t)
	body := channelPriceBody(`[{"model":"model-a","input":1,"output":2,"cache_read":0.1,"cache_write":0.2}]`)
	recorder := putChannelJSON(t, "ch-price", "", body)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var stored model.Channel
	require.NoError(t, model.DB.Where("name = ?", "ch-price").First(&stored).Error)
	prices := stored.GetSetting().PBRPrices
	require.Len(t, prices, 1)
	assert.Empty(t, prices[0].Timezone)
	assert.Empty(t, prices[0].Bands)

	var response map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Contains(t, response, "prices")
}
