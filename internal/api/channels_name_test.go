package api

import (
	"net/http"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/common"
	"github.com/zzyyyds88/PowerBarRations/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 历史重名渠道不能被静默解析成第一条：按名读取与车道成员保存都必须 409。
func TestAmbiguousChannelNameIsRejected(t *testing.T) {
	db := setupAPITestDB(t)
	require.NoError(t, db.Create(&model.Channel{Name: "dup-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m1"}).Error)
	require.NoError(t, db.Create(&model.Channel{Name: "dup-ch", Type: 1, Key: "sk", Status: common.ChannelStatusEnabled, Group: "default", Models: "m2"}).Error)

	recorder := callAPI(t, http.MethodGet, "/api/v1/channels/dup-ch", "", GetChannel,
		gin.Params{{Key: "name", Value: "dup-ch"}})
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"code":"channel_name_ambiguous"`)
	assert.Contains(t, recorder.Body.String(), `"count":2`)

	body := `{"members":[{"channel":"dup-ch","model":"m1","priority":1}]}`
	recorder = callAPI(t, http.MethodPut, "/api/v1/lanes/dup-lane", body, PutLane,
		gin.Params{{Key: "name", Value: "dup-lane"}})
	assert.Equal(t, http.StatusConflict, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `"code":"channel_name_ambiguous"`)
}
