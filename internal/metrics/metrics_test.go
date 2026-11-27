package metrics

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestPruneChannelStatesRemovesStaleGaugeSeries(t *testing.T) {
	m := newMetrics()
	m.setChannelState("42", circuitStateClosed, true)
	m.pruneChannelStates(map[string]struct{}{})

	families, err := m.registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != "pbr_channel_up" && family.GetName() != "pbr_circuit_state" {
			continue
		}
		for _, metric := range family.GetMetric() {
			if hasLabels(metric, map[string]string{"channel": "42"}) {
				t.Fatalf("stale channel series remains in %s: %s", family.GetName(), describeFamily(family))
			}
		}
	}
}

// scrape 通过 http.Handler 抓取一次指标文本。
func scrape(t *testing.T, handler http.Handler) (contentType, body string) {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics 状态码 = %d，want 200", rec.Code)
	}
	return rec.Header().Get("Content-Type"), rec.Body.String()
}

// scrapeFamilies 抓取并用 expfmt 解析为指标族，验证文本格式可被 Prometheus 解析。
func scrapeFamilies(t *testing.T, handler http.Handler) map[string]*dto.MetricFamily {
	t.Helper()
	contentType, body := scrape(t, handler)
	if !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q，want text/plain 前缀", contentType)
	}
	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Prometheus 文本解析失败: %v\n输出:\n%s", err, body)
	}
	return families
}

// findMetric 按名称与标签定位序列；缺失即失败并打印全部候选，便于排障。
func findMetric(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	family, ok := families[name]
	if !ok {
		t.Fatalf("指标 %s 未出现在抓取结果中", name)
	}
	for _, metric := range family.GetMetric() {
		if hasLabels(metric, labels) {
			return metric
		}
	}
	t.Fatalf("指标 %s 缺少标签 %v 的序列；实际序列: %s", name, labels, describeFamily(family))
	return nil
}

func hasLabels(metric *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(metric.GetLabel()))
	for _, pair := range metric.GetLabel() {
		got[pair.GetName()] = pair.GetValue()
	}
	for name, value := range want {
		if got[name] != value {
			return false
		}
	}
	return true
}

func describeFamily(family *dto.MetricFamily) string {
	var parts []string
	for _, metric := range family.GetMetric() {
		labels := make([]string, 0, len(metric.GetLabel()))
		for _, pair := range metric.GetLabel() {
			labels = append(labels, pair.GetName()+"="+pair.GetValue())
		}
		parts = append(parts, "{"+strings.Join(labels, ",")+"}")
	}
	return strings.Join(parts, " ")
}

func counterValue(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	return findMetric(t, families, name, labels).GetCounter().GetValue()
}

func gaugeValue(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	return findMetric(t, families, name, labels).GetGauge().GetValue()
}

func histogramOf(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) *dto.Histogram {
	t.Helper()
	return findMetric(t, families, name, labels).GetHistogram()
}

func assertNear(t *testing.T, got, want float64, what string) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v，want %v", what, got, want)
	}
}

// TestHandlerServesGoRuntimeMetrics 验证默认 Go runtime 指标与文本格式。
func TestHandlerServesGoRuntimeMetrics(t *testing.T) {
	families := scrapeFamilies(t, newMetrics().handler())
	if _, ok := families["go_goroutines"]; !ok {
		t.Fatal("抓取结果缺少 Go runtime 指标 go_goroutines")
	}
}

// TestHandlerWithoutObservations 验证未观测时输出空指标不报错。
func TestHandlerWithoutObservations(t *testing.T) {
	families := scrapeFamilies(t, newMetrics().handler())
	for _, name := range []string{
		"pbr_requests_total",
		"pbr_request_duration_seconds",
		"pbr_ttft_seconds",
		"pbr_tokens_total",
		"pbr_cost_total",
		"pbr_upstream_errors_total",
		"pbr_circuit_state",
		"pbr_channel_up",
	} {
		if _, ok := families[name]; ok {
			t.Fatalf("未观测时不应出现指标 %s", name)
		}
	}
}

// TestObserveRequestUpdatesMetrics 覆盖请求计数、耗时、TTFT、令牌、成本与上游错误。
func TestObserveRequestUpdatesMetrics(t *testing.T) {
	m := newMetrics()
	m.observeRequest(RequestObservation{
		Model: "gpt-test", Lane: "lane-a", Channel: "chan-a", Status: http.StatusOK,
		Duration: 2 * time.Second, TTFT: 500 * time.Millisecond,
		PromptTokens: 100, CompletionTokens: 20,
		CacheReadTokens: 5, CacheWriteTokens: 7, ReasoningTokens: 3,
		Cost: 0.12,
	})
	m.observeRequest(RequestObservation{
		Model: "gpt-test", Lane: "lane-a", Channel: "chan-b", Status: http.StatusInternalServerError,
		Duration:     time.Second,
		PromptTokens: 50, CompletionTokens: 10,
		Cost: 0.03, ErrorKind: "soft_transient",
	})
	families := scrapeFamilies(t, m.handler())

	successLabels := map[string]string{
		"model": "gpt-test", "lane": "lane-a", "channel": "chan-a", "status": "200",
	}
	if got := counterValue(t, families, "pbr_requests_total", successLabels); got != 1 {
		t.Fatalf("pbr_requests_total{200} = %v，want 1", got)
	}
	failureLabels := map[string]string{
		"model": "gpt-test", "lane": "lane-a", "channel": "chan-b", "status": "500",
	}
	if got := counterValue(t, families, "pbr_requests_total", failureLabels); got != 1 {
		t.Fatalf("pbr_requests_total{500} = %v，want 1", got)
	}

	duration := histogramOf(t, families, "pbr_request_duration_seconds", map[string]string{"model": "gpt-test"})
	if duration.GetSampleCount() != 2 {
		t.Fatalf("duration 样本数 = %d，want 2", duration.GetSampleCount())
	}
	assertNear(t, duration.GetSampleSum(), 3, "duration 样本和")
	if len(duration.GetBucket()) == 0 {
		t.Fatal("duration 直方图缺少分桶")
	}

	ttft := histogramOf(t, families, "pbr_ttft_seconds", map[string]string{"model": "gpt-test"})
	if ttft.GetSampleCount() != 1 {
		t.Fatalf("TTFT 样本数 = %d，want 1（非流式未观测不应计入）", ttft.GetSampleCount())
	}
	assertNear(t, ttft.GetSampleSum(), 0.5, "TTFT 样本和")

	tokenCases := []struct {
		kind string
		want float64
	}{
		{tokenKindPrompt, 150},
		{tokenKindCompletion, 30},
		{tokenKindCacheRead, 5},
		{tokenKindCacheWrite, 7},
		{tokenKindReasoning, 3},
	}
	for _, tc := range tokenCases {
		labels := map[string]string{"model": "gpt-test", "kind": tc.kind}
		if got := counterValue(t, families, "pbr_tokens_total", labels); got != tc.want {
			t.Fatalf("pbr_tokens_total{kind=%s} = %v，want %v", tc.kind, got, tc.want)
		}
	}

	costA := counterValue(t, families, "pbr_cost_total", map[string]string{"model": "gpt-test", "channel": "chan-a"})
	assertNear(t, costA, 0.12, "chan-a 成本")
	costB := counterValue(t, families, "pbr_cost_total", map[string]string{"model": "gpt-test", "channel": "chan-b"})
	assertNear(t, costB, 0.03, "chan-b 成本")

	if got := counterValue(t, families, "pbr_upstream_errors_total", map[string]string{"kind": "soft_transient"}); got != 1 {
		t.Fatalf("pbr_upstream_errors_total{soft_transient} = %v，want 1", got)
	}
}

// TestObserveRequestSkipsZeroValues 验证零值分项不生成高基数空序列。
func TestObserveRequestSkipsZeroValues(t *testing.T) {
	m := newMetrics()
	m.observeRequest(RequestObservation{Model: "plain-model", Status: http.StatusOK})
	families := scrapeFamilies(t, m.handler())

	if got := counterValue(t, families, "pbr_requests_total", map[string]string{
		"model": "plain-model", "lane": "unknown", "channel": "unknown", "status": "200",
	}); got != 1 {
		t.Fatalf("pbr_requests_total = %v，want 1", got)
	}
	for _, name := range []string{
		"pbr_request_duration_seconds",
		"pbr_ttft_seconds",
		"pbr_tokens_total",
		"pbr_cost_total",
		"pbr_upstream_errors_total",
	} {
		if _, ok := families[name]; ok {
			t.Fatalf("零值分项不应生成指标 %s", name)
		}
	}

	// 非正状态码按 unknown 记录，不产生空标签或伪造状态码。
	m.observeRequest(RequestObservation{Model: "no-status-model", Lane: "lane", Channel: "chan"})
	families = scrapeFamilies(t, m.handler())
	if got := counterValue(t, families, "pbr_requests_total", map[string]string{
		"model": "no-status-model", "lane": "lane", "channel": "chan", "status": "unknown",
	}); got != 1 {
		t.Fatalf("状态码缺失时 pbr_requests_total{status=unknown} = %v，want 1", got)
	}
}

// TestSetChannelState 验证熔断/可用状态快照与未知取值的保守处理。
func TestSetChannelState(t *testing.T) {
	m := newMetrics()
	m.setChannelState("chan-closed", circuitStateClosed, true)
	m.setChannelState("chan-half", circuitStateHalfOpen, true)
	m.setChannelState("chan-open", circuitStateOpen, false)
	families := scrapeFamilies(t, m.handler())

	cases := []struct {
		channel string
		want    float64
	}{
		{"chan-closed", 0},
		{"chan-half", 1},
		{"chan-open", 2},
	}
	for _, tc := range cases {
		if got := gaugeValue(t, families, "pbr_circuit_state", map[string]string{"channel": tc.channel}); got != tc.want {
			t.Fatalf("pbr_circuit_state{channel=%s} = %v，want %v", tc.channel, got, tc.want)
		}
	}
	if got := gaugeValue(t, families, "pbr_channel_up", map[string]string{"channel": "chan-closed"}); got != 1 {
		t.Fatalf("chan-closed up = %v，want 1", got)
	}
	if got := gaugeValue(t, families, "pbr_channel_up", map[string]string{"channel": "chan-open"}); got != 0 {
		t.Fatalf("chan-open up = %v，want 0", got)
	}

	// 未知熔断状态：只更新 channel_up，不伪造熔断值。
	m.setChannelState("chan-closed", "bogus-state", false)
	families = scrapeFamilies(t, m.handler())
	if got := gaugeValue(t, families, "pbr_circuit_state", map[string]string{"channel": "chan-closed"}); got != 0 {
		t.Fatalf("未知状态不应改动 pbr_circuit_state，got %v", got)
	}
	if got := gaugeValue(t, families, "pbr_channel_up", map[string]string{"channel": "chan-closed"}); got != 0 {
		t.Fatalf("未知熔断状态仍应同步 channel_up，got %v", got)
	}

	// 空渠道名不产生序列。
	m.setChannelState("", circuitStateOpen, true)
	families = scrapeFamilies(t, m.handler())
	family, ok := families["pbr_channel_up"]
	if !ok {
		t.Fatal("pbr_channel_up 应保留已设置的渠道")
	}
	for _, metric := range family.GetMetric() {
		if hasLabels(metric, map[string]string{"channel": ""}) {
			t.Fatal("空渠道名不应产生指标序列")
		}
	}
}

// TestPackageLevelWrappersUseDefaultMetrics 验证包级入口转发到默认实例。
func TestPackageLevelWrappersUseDefaultMetrics(t *testing.T) {
	const probe = "__metrics_default_probe__"
	labels := map[string]string{
		"model": probe, "lane": probe, "channel": probe, "status": "204",
	}
	// 默认实例是进程级的，同包重复执行时计数会累加；断言增量而不是绝对值，
	// 避免测试自己制造"全局状态不可重复"的假失败。首次执行时该序列还不存在。
	before := float64(0)
	if family, ok := scrapeFamilies(t, Handler())["pbr_requests_total"]; ok {
		for _, metric := range family.GetMetric() {
			if hasLabels(metric, labels) {
				before = metric.GetCounter().GetValue()
				break
			}
		}
	}
	ObserveRequest(RequestObservation{
		Model: probe, Lane: probe, Channel: probe, Status: http.StatusNoContent,
		Duration: time.Millisecond,
	})
	SetChannelState(probe, circuitStateHalfOpen, true)

	families := scrapeFamilies(t, Handler())
	if got := counterValue(t, families, "pbr_requests_total", labels); got != before+1 {
		t.Fatalf("默认实例请求计数 = %v，want %v", got, before+1)
	}
	if got := gaugeValue(t, families, "pbr_circuit_state", map[string]string{"channel": probe}); got != 1 {
		t.Fatalf("默认实例熔断状态 = %v，want 1", got)
	}
	if got := gaugeValue(t, families, "pbr_channel_up", map[string]string{"channel": probe}); got != 1 {
		t.Fatalf("默认实例渠道可用 = %v，want 1", got)
	}
}

// TestMetricFamiliesMatchContract 验证指标名、类型与标签集合严格符合 api-spec §5.1。
func TestMetricFamiliesMatchContract(t *testing.T) {
	m := newMetrics()
	m.observeRequest(RequestObservation{
		Model: "contract-model", Lane: "contract-lane", Channel: "contract-channel",
		Status: http.StatusOK, Duration: time.Second, TTFT: 100 * time.Millisecond,
		PromptTokens: 1, CompletionTokens: 1, CacheReadTokens: 1, CacheWriteTokens: 1, ReasoningTokens: 1,
		Cost: 0.01, ErrorKind: "soft_transient",
	})
	m.setChannelState("contract-channel", circuitStateClosed, true)
	families := scrapeFamilies(t, m.handler())

	cases := []struct {
		name   string
		typ    dto.MetricType
		labels []string
	}{
		{"pbr_requests_total", dto.MetricType_COUNTER, []string{"model", "lane", "channel", "status"}},
		{"pbr_request_duration_seconds", dto.MetricType_HISTOGRAM, []string{"model"}},
		{"pbr_ttft_seconds", dto.MetricType_HISTOGRAM, []string{"model"}},
		{"pbr_tokens_total", dto.MetricType_COUNTER, []string{"model", "kind"}},
		{"pbr_cost_total", dto.MetricType_COUNTER, []string{"model", "channel"}},
		{"pbr_upstream_errors_total", dto.MetricType_COUNTER, []string{"kind"}},
		{"pbr_circuit_state", dto.MetricType_GAUGE, []string{"channel"}},
		{"pbr_channel_up", dto.MetricType_GAUGE, []string{"channel"}},
	}
	for _, tc := range cases {
		family, ok := families[tc.name]
		if !ok {
			t.Fatalf("缺少指标 %s", tc.name)
		}
		if family.GetType() != tc.typ {
			t.Fatalf("指标 %s 类型 = %v，want %v", tc.name, family.GetType(), tc.typ)
		}
		if family.GetHelp() == "" {
			t.Fatalf("指标 %s 缺少 HELP 文本", tc.name)
		}
		metric := family.GetMetric()[0]
		got := make([]string, 0, len(metric.GetLabel()))
		for _, pair := range metric.GetLabel() {
			got = append(got, pair.GetName())
		}
		// 标签顺序在 Prometheus 文本解析后按字母排序，语义上无序；按集合比较。
		want := append([]string(nil), tc.labels...)
		sort.Strings(got)
		sort.Strings(want)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("指标 %s 标签 = %v，want %v", tc.name, got, want)
		}
	}
}
