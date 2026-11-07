package feature

import (
	"os"
	"testing"

	"go_wp/internal/pipeline"
)

// 业务组件的视图构建必须在首次编译前登记；生产装配在基础层调同一入口。
// 缺了它，含 core.nav / core.product 等业务组件的实例会以「不支持的组件类型」失败。
func TestMain(m *testing.M) {
	pipeline.RegisterBusinessViews()
	os.Exit(m.Run())
}
