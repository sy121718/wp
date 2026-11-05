package aidto

import (
	"fmt"
	"net/url"
	"strings"

	jsonx "go_wp/pkg/jsonx"
)

// ProviderDeleteReq 是「删除供应商」这一个普通写 API 的请求体。
//
// 为什么要一个类型而不是直接读 id：写端点的字段契约由 DTO 生成（cmd/write-contracts
// 扫 binding.JSON[T] 的参数类型），只从 query/form 里 bindID 的端点在契约里没有权威
// 字段来源 —— 于是要么缺条目、要么逼生成器硬写一个 id。这里让 JSON 与字段契约共用
// 同一个类型源。
type ProviderDeleteReq struct {
	ID int64 `json:"id" form:"id" binding:"gt=0"`
}

type SaveProviderReq struct {
	ID          int64  `json:"id" form:"id"`
	ProviderKey string `json:"providerKey" form:"providerKey"`
	DisplayName string `json:"displayName" form:"displayName"`
	BaseURL     string `json:"baseUrl" form:"baseUrl"`
	Protocol    string `json:"protocol" form:"protocol"`
	APIKey      string `json:"apiKey" form:"apiKey"`
	Status      *int   `json:"status" form:"status"`
	Sort        int    `json:"sort" form:"sort"`
	Version     int64  `json:"version" form:"version"`

	UpdateBy int64 `json:"-"`
}

type SetStatusReq struct {
	ID       int64 `json:"id" form:"id"`
	Status   int   `json:"status" form:"status"`
	Version  int64 `json:"version" form:"version"`
	UpdateBy int64 `json:"-"`
}

type SaveModelsReq struct {
	ProviderID int64 `json:"providerId" form:"providerId"`
	Version    int64 `json:"version" form:"version"`

	// form:"-" 是故意的：表格里的模型行是**平铺草稿字段**（modelId / input_text_N …），
	// 提交前由 ai-models.js 的 payload builder 组装成 models 这个 JSON 数组字符串。
	// 让 gin 按 form 直接绑 []ModelEntry 会在 ShouldBind 阶段就失败 —— 而 pkg/binding
	// 的顺序是 ShouldBind 先、FormBinder 后，所以 FormBinder 根本没机会跑。
	Models   []ModelEntry `json:"models" form:"-"`
	UpdateBy int64        `json:"-"`
}

// BindForm 让 multipart / urlencoded 提交也能拿到 models。
//
// 形状与 PRODUCT 域既有约定一致（internal/module/product/dto/product_variant_req.go
// 的 SaveVariantListReq.BindForm）：数组一律以 JSON 字符串放在单个 form 字段里，
// 由 DTO 自己解析。这里不扩共享 binding 的语义，也不另造一套表单协议。
func (r *SaveModelsReq) BindForm(vals url.Values) error {
	raw := strings.TrimSpace(vals.Get("models"))
	if raw == "" {
		return nil
	}
	if err := jsonx.Unmarshal([]byte(raw), &r.Models); err != nil {
		// 带上字段形状：裸 json 语法错误（SyntaxError）没有字段可指，写基座只会回一句
		// 「请求参数错误」，排查时看不出是 models 这一段坏了。
		return fmt.Errorf("models: %w", err)
	}
	return nil
}

// AppendModelsReq 是「把勾选的模型追加进目录」的请求。
//
// ModelIDs 是前端勾选出来的 id 原始列表（可重复、可带空白），去重与上限由 service 负责 ——
// controller 只绑定，不在绑定层做业务判断。
type AppendModelsReq struct {
	ProviderID int64    `json:"providerId" form:"providerId"`
	Version    int64    `json:"version" form:"version"`
	ModelIDs   []string `json:"modelIds" form:"modelIds"`
	UpdateBy   int64    `json:"-"`
}

type ProviderActionReq struct {
	ProviderID int64 `json:"providerId" form:"providerId"`
	Version    int64 `json:"version" form:"version"`
	UpdateBy   int64 `json:"-"`
}

type ChatReq struct {
	ProviderKey     string `json:"provider_key" form:"provider_key" binding:"required,max=50"`
	Model           string `json:"model"        form:"model"        binding:"required,max=200"`
	Input           string `json:"input"        form:"input"        binding:"required,max=200000"`
	MaxOutputTokens int64  `json:"max_output_tokens" form:"max_output_tokens"`

	SessionID int64 `json:"session_id" form:"session_id"`
	UserID    int64 `json:"user_id"    form:"user_id"`

	Messages []ChatMessage `json:"-" form:"-"`

	Tools []ToolSpec `json:"-" form:"-"`
}
