package model

import (
	"github.com/zzyyyds88/PowerBarRations/common"
)

// 配额批量写回（BatchUpdate* 系列）随 User 表配额列物理删除（design-v1 §3.4）：
// users.quota / used_quota 不再有任何写方，聚合器与 InitBatchUpdater 一并移除；
// 这里只保留 Redis 缓存回填的判定辅助。

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
