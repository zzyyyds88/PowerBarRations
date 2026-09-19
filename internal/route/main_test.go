package route

import (
	"errors"
	"os"
	"testing"

	"github.com/zzyyyds88/PowerBarRations/model"
)

// unitTestResolveErr 单元测试没有 DB：默认把"每轮重读车道配置"（routing-spec §3）
// 的解析入口替换成**报错**，reloadLocked 据此保留旧快照——与热更新引入前的行为
// 一致，让既有夹具（手搓 ResolvedRoute）不受影响。热更新用例各自覆盖该变量。
var unitTestResolveErr = errors.New("route: no lane store in unit tests")

// stubResolveRouteForReload 是测试期的默认重读实现；热更新用例用 t.Cleanup 还原到它。
func stubResolveRouteForReload(string) (*model.ResolvedRoute, error) {
	return nil, unitTestResolveErr
}

func TestMain(m *testing.M) {
	resolveRouteForReload = stubResolveRouteForReload
	os.Exit(m.Run())
}
