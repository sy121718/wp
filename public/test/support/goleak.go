// goleak.go — TestMain 模式的 goroutine 泄漏检测封装（go.uber.org/goleak）。
//
// 与 testcontainers 容器清理的顺序契约：
//  1. m.Run() 返回时，所有 Test 的 t.Cleanup 已执行（每测试的 schema DROP、
//     Redis/auth 组件关闭等）；
//  2. 随后 ShutdownSharedTestEnv 终止共享容器（清理先于 leak check）；
//  3. 最后才做 goleak.Find，容器后台 goroutine 不会参与泄漏判定。
//
// 用法（包内 main_test.go）：
//
//	func TestMain(m *testing.M) {
//	    os.Exit(support.RunTestMainWithLeakCheck(m))
//	}
package support

import (
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

// RunTestMainWithLeakCheck 运行全部测试并在资源清理完成后检测 goroutine 泄漏，
// 返回进程退出码（有泄漏且原退出码为 0 时置 1）。
// ignore 透传给 goleak.Find，用于过滤已知良性后台 goroutine。
func RunTestMainWithLeakCheck(m *testing.M, ignore ...goleak.Option) int {
	code := m.Run()

	// 共享容器必须在泄漏检测前终止，否则清理 goroutine 会被误判为泄漏。
	ShutdownSharedTestEnv()

	if err := goleak.Find(ignore...); err != nil {
		fmt.Fprintf(os.Stderr, "goleak: 检测到 goroutine 泄漏: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
