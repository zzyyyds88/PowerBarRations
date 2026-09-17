package common

import "github.com/zzyyyds88/PowerBarRations/constant"

const defaultAnonymousRequestBodyLimitKB = 2048

func GetAnonymousRequestBodyLimitBytes() int64 {
	limitKB := constant.AnonymousRequestBodyLimitKB
	if limitKB < 0 {
		limitKB = defaultAnonymousRequestBodyLimitKB
	}
	return int64(limitKB) << 10
}
