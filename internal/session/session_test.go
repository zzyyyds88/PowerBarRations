package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueVerifyRoundTrip(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	material := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	token, err := Issue(material, time.Hour, now)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	require.NoError(t, Verify(token, material, now))
	// 有效期内任意时刻都应通过。
	require.NoError(t, Verify(token, material, now.Add(59*time.Minute)))
	// 到期即失效。
	require.ErrorIs(t, Verify(token, material, now.Add(2*time.Hour)), ErrInvalid)
}

// 改口令 → 库内哈希变化 → 旧会话必须立即失效（这是会话安全的关键性质）。
func TestVerifyFailsAfterCredentialChange(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	oldMaterial := "old-hash-0000000000000000000000000000000000000000000000000000000000"
	newMaterial := "new-hash-1111111111111111111111111111111111111111111111111111111111"
	token, err := Issue(oldMaterial, time.Hour, now)
	require.NoError(t, err)
	require.NoError(t, Verify(token, oldMaterial, now))
	require.ErrorIs(t, Verify(token, newMaterial, now), ErrInvalid)
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	material := "material-00000000000000000000000000000000000000000000000000000000"
	token, err := Issue(material, time.Hour, now)
	require.NoError(t, err)

	// 篡改签名。
	require.ErrorIs(t, Verify(token+"x", material, now), ErrInvalid)
	// 截断 / 结构错误。
	require.ErrorIs(t, Verify("only.two", material, now), ErrInvalid)
	require.ErrorIs(t, Verify("", material, now), ErrInvalid)
	// 密钥不匹配。
	require.ErrorIs(t, Verify(token, "", now), ErrInvalid)
	// 指纹位被改写。
	parts := []rune(token)
	parts[0] = differentRune(parts[0])
	require.ErrorIs(t, Verify(string(parts), material, now), ErrInvalid)
}

func differentRune(r rune) rune {
	if r == 'A' {
		return 'B'
	}
	return 'A'
}

func TestIssueRequiresMaterial(t *testing.T) {
	_, err := Issue("", time.Hour, time.Now())
	require.Error(t, err)
}

// Cookie 属性：HttpOnly 必须为真（JS 不可读），Secure 按配置。
func TestSetCookieIsHttpOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetCookie(recorder, "tok", time.Hour, true)
	resp := recorder.Result()
	defer resp.Body.Close()
	cookies := resp.Cookies()
	require.Len(t, cookies, 1)
	cookie := cookies[0]
	assert.Equal(t, CookieName, cookie.Name)
	assert.True(t, cookie.HttpOnly, "会话 Cookie 必须 HttpOnly")
	assert.True(t, cookie.Secure)
	assert.Equal(t, "/", cookie.Path)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
}

func TestClearCookieExpires(t *testing.T) {
	recorder := httptest.NewRecorder()
	ClearCookie(recorder, false)
	cookies := recorder.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, -1, cookies[0].MaxAge)
	assert.Empty(t, cookies[0].Value)
}

func TestTokenFromRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	assert.Empty(t, TokenFromRequest(req))
	req.AddCookie(&http.Cookie{Name: CookieName, Value: "abc"})
	assert.Equal(t, "abc", TokenFromRequest(req))
}
