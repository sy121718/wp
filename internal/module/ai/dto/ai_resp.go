package aidto

import "go_wp/pkg/utils"

// ai_resp.go — ai 模块的响应形状（service → inbound）。
//
// 对外形状不借用 model 层实体（契约与存储解耦）：model 只回实体，service 负责翻译。

// ModelEntry 模型目录里的一行（config_data.models 的元素形状）。
//
// 接口形状用 camelCase（与其它后台 JSON 接口同口径），**落库形状是 snake_case**
// （id / display_name / context_window / max_output_tokens / input_types，见迁移 511
// 文件头）：两处字段名不同，翻译在 service 的 parseModels / modelsToConfig 一对函数里 ——
// 改这里的 JSON 名不会动落库名，反之亦然，别只改一边。
//
// ContextWindow / MaxOutputTokens 为 0 表示**未知**（拉取回来的目录只有 id，
// 元数据留空由管理员按需补），不是「0 token」的语义 —— 展示侧据此显示占位。
type ModelEntry struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"displayName"`
	ContextWindow   int64    `json:"contextWindow"`
	MaxOutputTokens int64    `json:"maxOutputTokens"`
	InputTypes      []string `json:"inputTypes"`
}

// Provider 一个供应商的完整视图（含模型目录）。
//
// **不含密钥明文**：只有 HasAPIKey（是否已配置）—— 密钥出库即加密，回读永远是布尔。
type Provider struct {
	ID          int64          `json:"id"`
	ProviderKey string         `json:"providerKey"`
	DisplayName string         `json:"displayName"`
	BaseURL     string         `json:"baseUrl"`
	Protocol    string         `json:"protocol"`
	Status      int            `json:"status"`
	Sort        int            `json:"sort"`
	Models      []ModelEntry   `json:"models"`
	HasAPIKey   bool           `json:"hasApiKey"`
	Version     int64          `json:"version"`
	UpdateTime  utils.JSONTime `json:"updateTime"`
}

// FetchModelsResult 「获取可用模型」的结果：落库后的供应商 + 本次新增的模型 ID。
//
// 返回 Added 而不是「只回供应商」：页面要告诉用户这次到底拉回了什么，
// 否则「获取可用模型」点了没反应（全已存在）与「拉了 0 个」看起来一样。
type FetchModelsResult struct {
	Provider *Provider `json:"provider"`
	Added    []string  `json:"added"`
	Fetched  int       `json:"fetched"`
}

// ModelCandidates 「获取可用模型」的候选集：**只拉取、不落库**，交用户在弹窗里勾选。
//
// 与 FetchModelsResult 的分工：那个是「拉完直接并进目录」（JSON 接口的历史行为），
// 这个是「把候选交出去」—— 页面侧只有在用户点「添加所选」之后才写库，
// 所以拉取本身不能有副作用（点开弹窗看一眼不该改动目录）。
//
// Candidates 是服务端返回的**原始 id 列表**（不排序、不加工、不剥前缀）；
// Existing 是目录里已有的 id，供弹窗标记「已在目录」——
// 有它前端才能把「已存在」与「这次新增」分开显示，而不是一律灰掉。
type ModelCandidates struct {
	Provider   *Provider `json:"provider"`
	Candidates []string  `json:"candidates"`
	Existing   []string  `json:"existing"`
	Fetched    int       `json:"fetched"`
}
