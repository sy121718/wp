package utils

import "testing"

// TestIsTestProcess 金丝雀：本用例**必然**运行在 go test 里，判据却返回 false 就说明
// 探测坏了 —— 而坏掉的直接后果是测试进程自动跑生产调度器（删真实开发库的历史数据）。
func TestIsTestProcess(t *testing.T) {
	if !IsTestProcess() {
		t.Fatal("运行在 go test 里但 IsTestProcess()=false：探测失效，测试进程会启动生产调度器")
	}
}
