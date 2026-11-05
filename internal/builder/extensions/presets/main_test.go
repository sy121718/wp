package presets

import (
	"os"
	"testing"

	"go_wp/internal/builder/extensions/views"
)

// TestMain 先登记业务组件的视图构建：预设校验会按真实组件类型检查。
func TestMain(m *testing.M) {
	views.RegisterAll()
	os.Exit(m.Run())
}
