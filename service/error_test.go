package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/relaykit/types"
)

func TestResetStatusCode(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		statusCode       int
		statusCodeConfig string
		expectedMapped   int
	}{
		{
			name:             "map string value to non-reserved code",
			statusCode:       429,
			statusCodeConfig: `{"429":"502"}`,
			expectedMapped:   502,
		},
		{
			name:             "map int value to non-reserved code",
			statusCode:       429,
			statusCodeConfig: `{"429":502}`,
			expectedMapped:   502,
		},
		{
			// 审查 B6：503 是模型面「没有可用渠道」的保留码，禁止被渠道映射占用。
			name:             "refuse mapping to reserved 503 (string)",
			statusCode:       429,
			statusCodeConfig: `{"429":"503"}`,
			expectedMapped:   0,
		},
		{
			name:             "refuse mapping to reserved 503 (int)",
			statusCode:       500,
			statusCodeConfig: `{"500":503}`,
			expectedMapped:   0,
		},
		{
			name:             "skip invalid string value",
			statusCode:       429,
			statusCodeConfig: `{"429":"bad-code"}`,
			expectedMapped:   0,
		},
		{
			name:             "skip status code 200",
			statusCode:       200,
			statusCodeConfig: `{"200":502}`,
			expectedMapped:   0,
		},
		{
			// ADR 0013：2xx 一律不是合法目标码。
			name:             "refuse mapping to 2xx",
			statusCode:       429,
			statusCodeConfig: `{"429":"200"}`,
			expectedMapped:   0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			newAPIError := &types.NewAPIError{
				StatusCode: tc.statusCode,
			}
			ResetStatusCode(newAPIError, tc.statusCodeConfig)
			require.Equal(t, tc.statusCode, newAPIError.StatusCode,
				"映射不得改写原始状态码：分类/冷却/熔断依赖它")
			require.Equal(t, tc.expectedMapped, newAPIError.MappedStatusCode)
			if tc.expectedMapped > 0 {
				require.Equal(t, tc.expectedMapped, newAPIError.ClientStatusCode())
			} else {
				require.Equal(t, tc.statusCode, newAPIError.ClientStatusCode())
			}
		})
	}
}

func TestValidateStatusCodeMapping(t *testing.T) {
	t.Parallel()
	valid := []string{
		"",
		"{}",
		`{"429":"400"}`,
		`{"500":502,"429":400}`,
	}
	for _, raw := range valid {
		require.NoError(t, ValidateStatusCodeMapping(raw), "应接受 %q", raw)
	}
	invalid := []string{
		`not-json`,
		`[]`,
		`{"abc":"400"}`,
		`{"429":"200"}`,
		`{"429":"503"}`,
		`{"429":"99"}`,
		`{"429":"bad"}`,
	}
	for _, raw := range invalid {
		require.Error(t, ValidateStatusCodeMapping(raw), "应拒绝 %q", raw)
	}
}

func TestRelayErrorHandlerTruncatesInvalidJSONBodyInLog(t *testing.T) {
	withDebugEnabled(t, false)

	body := strings.Repeat("b", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, "bad response status code 500", newAPIError.Error())
	require.Contains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), fmt.Sprintf("original_length=%d", len(body)))
	require.NotContains(t, logBuffer.String(), strings.Repeat("b", common.LocalLogContentLimit+1))
}

func TestRelayErrorHandlerKeepsStructuredErrorMessage(t *testing.T) {
	message := strings.Repeat("c", common.LocalLogContentLimit+256)
	body := `{"message":"` + message + `"}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsOpenAIErrorMessage(t *testing.T) {
	message := strings.Repeat("d", common.LocalLogContentLimit+256)
	body := `{"error":{"message":"` + message + `","type":"server_error","code":"server_error"}}`
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.Equal(t, message, newAPIError.Error())
}

func TestRelayErrorHandlerKeepsInvalidJSONBodyInDebugLog(t *testing.T) {
	withDebugEnabled(t, true)

	body := strings.Repeat("e", common.LocalLogContentLimit+256)
	var logBuffer bytes.Buffer

	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &logBuffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = oldWriter
		common.LogWriterMu.Unlock()
	})

	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	newAPIError := RelayErrorHandler(context.Background(), resp, false)

	require.NotNil(t, newAPIError)
	require.NotContains(t, logBuffer.String(), "[truncated")
	require.Contains(t, logBuffer.String(), body)
}

func withDebugEnabled(t *testing.T, enabled bool) {
	t.Helper()

	oldDebug := common.DebugEnabled
	common.DebugEnabled = enabled
	t.Cleanup(func() {
		common.DebugEnabled = oldDebug
	})
}

func TestParseRetryAfterHeader(t *testing.T) {
	require.Equal(t, 0, parseRetryAfterHeader(""))
	require.Equal(t, 0, parseRetryAfterHeader("0"))
	require.Equal(t, 0, parseRetryAfterHeader("-5"))
	require.Equal(t, 0, parseRetryAfterHeader("garbage"))
	require.Equal(t, 30, parseRetryAfterHeader("30"))

	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfterHeader(future)
	require.GreaterOrEqual(t, got, 85)
	require.LessOrEqual(t, got, 90)

	past := time.Now().Add(-90 * time.Second).UTC().Format(http.TimeFormat)
	require.Equal(t, 0, parseRetryAfterHeader(past), "过期时间不得产生负等待")
}
