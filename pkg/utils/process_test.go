package utils

import "testing"

func TestIsTestProcess(t *testing.T) {
	if !IsTestProcess() {
		t.Fatal("运行在 go test 里但 IsTestProcess()=false：探测失效，测试进程会启动生产调度器")
	}
}
