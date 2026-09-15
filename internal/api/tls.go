package api

import (
	"net/http"
	"strings"

	"pbr/internal/apierr"
	"pbr/internal/tlsutil"

	"github.com/gin-gonic/gin"
)

// HTTPS / 证书管理（README §5.1；契约登记在 OpenAPI /api/v1/openapi.json）：
//
//	GET  /api/v1/tls               查看当前证书状态（来源/SAN/有效期/指纹）
//	PUT  /api/v1/tls/certificate   导入运维提供的证书+私钥（PEM），写盘并热加载
//	POST /api/v1/tls/self-signed   生成/重新生成自签证书
//
// 说明：证书替换通过 tls.Config.GetCertificate 在下次握手生效，无需重启进程。

// GetTLSStatus GET /api/v1/tls
func GetTLSStatus(c *gin.Context) {
	c.JSON(http.StatusOK, tlsutil.Default.Status())
}

// PutTLSCertificate PUT /api/v1/tls/certificate，body {"cert_pem":"...","key_pem":"..."}
func PutTLSCertificate(c *gin.Context) {
	var body map[string]string
	if err := c.ShouldBindJSON(&body); err != nil {
		apierr.BadRequest(c, "invalid json body")
		return
	}
	certPEM := strings.TrimSpace(body["cert_pem"])
	keyPEM := strings.TrimSpace(body["key_pem"])
	if certPEM == "" || keyPEM == "" {
		apierr.Validation(c, "cert_pem and key_pem are required")
		return
	}
	if err := tlsutil.Default.Import([]byte(body["cert_pem"]), []byte(body["key_pem"])); err != nil {
		apierr.Validation(c, err.Error())
		return
	}
	writeAudit(c, "update", "tls_certificate", "imported")
	c.JSON(http.StatusOK, tlsutil.Default.Status())
}

// PostTLSSelfSigned POST /api/v1/tls/self-signed，body {"hosts":["a","1.2.3.4"],"days":825}
func PostTLSSelfSigned(c *gin.Context) {
	var body map[string]any
	_ = c.ShouldBindJSON(&body)
	hosts := make([]string, 0, 4)
	if raw, ok := body["hosts"].([]any); ok {
		for _, item := range raw {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				hosts = append(hosts, strings.TrimSpace(text))
			}
		}
	}
	if len(hosts) == 0 {
		hosts = tlsutil.DefaultHosts()
	}
	days := 825
	if value, ok := body["days"].(float64); ok && value > 0 {
		days = int(value)
	}
	if err := tlsutil.Default.RegenerateSelfSigned(hosts, days); err != nil {
		apierr.Validation(c, err.Error())
		return
	}
	writeAudit(c, "update", "tls_certificate", "self-signed")
	c.JSON(http.StatusOK, tlsutil.Default.Status())
}
