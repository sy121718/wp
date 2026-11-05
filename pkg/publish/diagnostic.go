package publish

// Diagnostic 是发布内核接收的降级诊断事实。
//
// 发布内核只收编译结果与诊断事实，由应用编译回调把构建器类型转换成这里的中立形状。
type Diagnostic struct {
	Slot     string `json:"slot,omitempty"`
	NodeID   string `json:"nodeId,omitempty"`
	NodeType string `json:"nodeType,omitempty"`
	Kind     string `json:"kind"`
	RefID    string `json:"refId"`
	Reason   string `json:"reason"`
}
