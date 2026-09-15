package helper

import (
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// RewriteModelField 把响应体里的 model 字段改写为指定值。
//
// 用途：design-v1 §4.1 要求"请求成功时响应体 model 回填请求名，不填上游真名"。
// 车道成员可以改名（LaneMember.UpstreamModel），思考后缀也会让上游真名与请求名不同，
// 二者都会让上游返回它自己的模型名，因此需要在这里回填。
//
// 只在确实需要改写时调用（调用方先比较两个名字），正常隐式路由下不开销。
// 解析失败时原样返回，绝不因为回填失败而破坏一个本来成功的响应。
func RewriteModelField(data []byte, model string) []byte {
	if len(data) == 0 || model == "" {
		return data
	}
	if !gjson.GetBytes(data, "model").Exists() {
		return data
	}
	rewritten, err := sjson.SetBytes(data, "model", model)
	if err != nil {
		return data
	}
	return rewritten
}
