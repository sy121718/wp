package feature

import (
	"os"
	"testing"

	"go_wp/public/test/support"
)

// TestMain 本包测试统一入口：全部测试（含各 t.Cleanup 资源清理）结束后，
// 先终止共享 testcontainers 容器，再做 goroutine 泄漏检测
// （顺序契约见 support/goleak.go）。
func TestMain(m *testing.M) {
	os.Exit(support.RunTestMainWithLeakCheck(m))
}
