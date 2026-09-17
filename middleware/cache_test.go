package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// SPA 的 HTML 入口（含深链回落的 index.html）必须 no-cache：
// 否则浏览器会缓存旧 index.html，服务端换了新 bundle 仍加载旧 JS
// （真实踩过：一键固化入口已从代码移除，界面却仍显示）。
func TestCacheNeverCachesHtmlButCachesHashedAssets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		uri  string
		want string
	}{
		{"/", "no-cache"},
		{"/routes", "no-cache"},
		{"/channels", "no-cache"},
		{"/index.html", "no-cache"},
		{"/static/js/index.abc123.js", "public, max-age=604800, immutable"},
		{"/static/css/index.abc123.css", "public, max-age=604800, immutable"},
	}
	for _, tc := range cases {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, tc.uri, nil)
		Cache()(c)
		if got := recorder.Header().Get("Cache-Control"); got != tc.want {
			t.Errorf("uri %s: Cache-Control = %q, want %q", tc.uri, got, tc.want)
		}
	}
}
