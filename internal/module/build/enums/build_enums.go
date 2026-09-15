// Package buildenums 统一管理 build 模块业务消息。
package buildenums

const (
	ErrInvalidParam    = "ErrInvalidParam"    // 请求参数无效
	ErrJobNotFound     = "ErrJobNotFound"     // 构建任务不存在
	ErrExecutorMissing = "ErrExecutorMissing" // 该来源类型没有注册执行器
	ErrInvalidSource   = "ErrInvalidSource"   // 来源类型不在白名单内
)

const (
	MsgQueueFound = "MsgQueueFound" // 队列状态查询成功
	MsgJobRetried = "MsgJobRetried" // 失败任务已重新入队
)
