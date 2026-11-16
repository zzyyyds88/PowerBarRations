package operation_setting

// W7 惰性遗留：Price/USDExchangeRate 不是支付执行，而是被保留的日志成本展示
// （logger/logger.go 的 $/¥ 折算）与保留路由读取的货币展示参数。原定义在
// payment_setting_old.go，支付设置物理删除后在此保留。
var Price = 7.3
var USDExchangeRate = 7.3
