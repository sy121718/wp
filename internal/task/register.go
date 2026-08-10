package task

import (
	"sync"

	"go_wp/pkg/queue"
)

var registerOnce sync.Once

// RegisterHandlers 显式注册项目内的任务处理器。
//
// 边界说明：
// - 这里只负责“项目私有任务”的注册
// - 任务处理器注册属于业务调度层，不属于框架基础组件启动层
// - queue 组件的 Init/Ready/Close 必须留在 pkg/queue
// - internal/task 只表达任务类型、任务处理逻辑和任务编排，不承担组件生命周期职责
func RegisterHandlers() {
	registerOnce.Do(func() {
		queue.Register(TypeEmailSend, HandleEmailSend)
		queue.Register(TypeOrderCancel, HandleOrderCancel)
	})
}
