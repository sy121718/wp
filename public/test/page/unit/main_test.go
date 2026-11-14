package unit

import (
	"os"
	"testing"

	"go_wp/internal/pipeline"
)

// 业务组件的视图构建必须在首次编译前登记；生产装配在 buildFoundation 里做同一件事。
// 缺了它，含 core.nav 等业务组件的页面会以「不支持的组件类型」失败 —— 这里补的是
// 测试夹具与生产装配的差异，不是给生产加兼容分支。
func TestMain(m *testing.M) {
	pipeline.RegisterBusinessViews()
	os.Exit(m.Run())
}
