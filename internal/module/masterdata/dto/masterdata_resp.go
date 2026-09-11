// masterdata_resp.go — 主数据变更记录模块出参（service 返回用，issue #19）。
package masterdatadto

// ChangeResp 一条字段级变更。
//
// 同时给出「取值」与「展示文案」：模板与前端不需要自己维护一份字段名到中文的映射
// （映射在模块 enums，一处维护）。
type ChangeResp struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`
	// EntityLabel 实体展示名快照（记录时的商品名 / SKU 编码 / 货源名）。
	EntityLabel string `json:"entityLabel"`
	Action      string `json:"action"`
	ActionLabel string `json:"actionLabel"`
	Field       string `json:"field"`
	FieldLabel  string `json:"fieldLabel"`
	OldValue    string `json:"oldValue"`
	NewValue    string `json:"newValue"`
	Origin      string `json:"origin"`
	OperatorID  string `json:"operatorId"`
	CreatedAt   string `json:"createdAt"`
}

// EntityHistoryResp 一个实体的变更历史概要（验收 4：后台可按实体查询）。
type EntityHistoryResp struct {
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`
	EntityLabel     string `json:"entityLabel"`
	ChangeCount     int64  `json:"changeCount"`
	LastAction      string `json:"lastAction"`
	LastActionLabel string `json:"lastActionLabel"`
	LastField       string `json:"lastField"`
	LastFieldLabel  string `json:"lastFieldLabel"`
	LastOperatorID  string `json:"lastOperatorId"`
	LastAt          string `json:"lastAt"`
}

// EntityTimelineResp 单个实体的变更时间线。
type EntityTimelineResp struct {
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`
	// EntityLabel 以最新一条记录的实体名快照为准（实体已删除时取删除时的快照）。
	EntityLabel string        `json:"entityLabel"`
	Total       int64         `json:"total"`
	Changes     []*ChangeResp `json:"changes"`
}
