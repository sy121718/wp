package projectdto

import (
	"encoding/json"
	"go_wp/pkg/utils"
)

// ProjectResp 站点工程响应。
type ProjectResp struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Settings  json.RawMessage `json:"settings"`
	CreatedAt utils.JSONTime  `json:"createdAt"`
	UpdatedAt utils.JSONTime  `json:"updatedAt"`
}

// RetentionPolicyResp 工程级数据保留策略。
//
// 这一列（`projects.analytics_retention_days`，迁移 161 加的）物理上长在 project 模块的
// 表上，语义上却是「访问明细保留几天」—— 所以由 project 模块读出、由 analytics 模块经契约
// 消费，而不是让 analytics 的 model 直接查 projects 表。表的所有权跟着表走，读法就跟着
// 所有权走：否则每多一个消费者，就多一份「列出工程 / 读 projects 某列」的 SQL 各自演化。
type RetentionPolicyResp struct {
	ProjectID     string `json:"projectId"`
	RetentionDays int    `json:"retentionDays"`
}
