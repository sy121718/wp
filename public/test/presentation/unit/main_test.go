package unit

import (
	"os"
	"testing"

	"go_wp/internal/pipeline"
)

// 业务组件的视图构建必须在首次编译前登记（生产装配在基础层调同一入口）。
func TestMain(m *testing.M) {
	pipeline.RegisterBusinessViews()
	os.Exit(m.Run())
}
