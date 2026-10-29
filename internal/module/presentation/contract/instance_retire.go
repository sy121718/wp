package presentationcontract

import "context"

// InstanceRetirer 只撤销当前访问状态并软删实例，保留不可变文档、产物与发布尝试历史。
// 控制面在确认源实体已无法恢复后显式调用，不在访客请求中自动处置。
type InstanceRetirer interface {
	RetireInstance(ctx context.Context, projectID, instanceID string) error
}
