package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zzyyyds88/PowerBarRations/common"
)

// AnonymousRequestBodyLimit 限制非模型面请求体大小（管理面 /api 与 PBR 管理 API）。
//
// 模型面（/v1/**）有自己的 128MB 上限（common.GetRequestBody / DecompressRequestMiddleware），
// 不走这里；本中间件只服务管理面，防止免鉴权的 setup/login 被超大 JSON 撑爆内存。
func AnonymousRequestBodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		maxBytes := common.GetAnonymousRequestBodyLimitBytes()
		if maxBytes <= 0 || c.Request.Body == nil {
			c.Next()
			return
		}

		originalBody := c.Request.Body
		limitedBody, err := readAnonymousRequestBody(originalBody, maxBytes)
		_ = originalBody.Close()
		if err != nil {
			if common.IsRequestBodyTooLargeError(err) {
				c.AbortWithStatus(http.StatusRequestEntityTooLarge)
				return
			}
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewReader(limitedBody))
		c.Request.ContentLength = int64(len(limitedBody))
		c.Next()
	}
}

func readAnonymousRequestBody(body io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, common.ErrRequestBodyTooLarge
	}
	return data, nil
}
