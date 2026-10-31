package support

import (
	"fmt"
	"os"
	"testing"

	"go.uber.org/goleak"
)

func RunTestMainWithLeakCheck(m *testing.M, ignore ...goleak.Option) int {
	code := m.Run()

	ShutdownSharedTestEnv()

	if err := goleak.Find(ignore...); err != nil {
		fmt.Fprintf(os.Stderr, "goleak: 检测到 goroutine 泄漏: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}
