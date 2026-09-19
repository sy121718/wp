package utils

// process.go — 进程形态探测。目前只回答一个问题：这段代码是不是跑在 go test 的二进制里。

import "flag"

// testFlagName go test 生成的测试二进制必然注册的 flag 名（testing 包在包级初始化时注册）。
const testFlagName = "test.v"

// IsTestProcess 当前进程是否是 go test 起的测试二进制。
//
// 判据是 flag.Lookup("test.v") != nil：testing 包在包级 init 时就会注册它，而任何测试
// 二进制都必然链接 testing（_test.go 里 import 了它）。刻意不用 testing.Testing()：
// 那会让生产二进制也链接 testing，顺带注册一整套 -test.* 参数 —— 为一个布尔值改掉
// 二进制的命令行接口，不值得。
//
// 它是**启发式**，边界说清楚：手工注册了 test.v 的普通 main 会被误判；go test 起的
// 一定命中。调用方只有一类 —— 「调度器要不要在装配时自动开跑」这类**只影响副作用、
// 不影响正确性**的开关：误判的代价是「调度器没起」，不是「数据坏了」。
//
// 为什么需要它（2026-09-19 第四批）：后台调度器（保留期清理 / 回执收敛 / 对账巡检 /
// 订单过期）都在各模块路由装配时启动，而测试引导（public/test/support 的 SetupRoutes）
// 走的是**同一条装配路径** —— 不拦的话，go test 的每个包都会首跑一轮调度：
// 保留期清理会**删真实开发库的历史数据**，媒体对账要遍历真实存储。
// 测试的可观测行为必须由用例自己触发，不该有后台 goroutine 抢着做。
func IsTestProcess() bool {
	return flag.Lookup(testFlagName) != nil
}
