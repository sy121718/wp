package pipeline

import (
	"sync"

	"go_wp/internal/builder/extensions/views"
)

var registerViewsOnce sync.Once

// RegisterBusinessViews 把业务组件的视图构建函数登记到工具内核。
//
// 必须在首次编译/组件目录缓存之前调用一次；重复调用是幂等的。
func RegisterBusinessViews() {
	registerViewsOnce.Do(views.RegisterAll)
}
