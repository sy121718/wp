// ai_service.go — ai 模块的对外能力契约。
//
// 契约里只有「别的模块 / 装配层能用它做什么」。本模块的存储形状与内部助手不出现在这里
// （越权防护靠**接口形状**而不是调用方自觉）。
//
// 当前没有跨模块消费者：方法是给装配层与后台页面用的完整能力面；将来别的模块要「按
// provider_key 取一个可用模型」，再在这里加一个收窄端口（而不是把内部 Service 暴露出去）。
package aicontract

import (
	"context"

	"go_wp/internal/module/ai/dto"
)

// AIService AI 供应商 / 模型配置的能力面。
//
// 密钥口径（对调用方的硬承诺）：**任何方法都不返回密钥明文**，只回 HasAPIKey。
// 唯一读取明文的路径是拉取可用模型时在 service 内部解密后放进请求头。
type AIService interface {
	// ListProviders 列出全部供应商（含各自的模型目录）。
	ListProviders(ctx context.Context) ([]aidto.Provider, error)
	// GetProvider 按主键取一个供应商。
	GetProvider(ctx context.Context, id int64) (*aidto.Provider, error)
	// SaveProvider 新建（ID == 0）或按版本号更新一个供应商。
	SaveProvider(ctx context.Context, req *aidto.SaveProviderReq) (*aidto.Provider, error)
	// DeleteProvider 删除一个供应商（含其模型目录）。
	DeleteProvider(ctx context.Context, id int64) error
	// SetProviderStatus 启停一个供应商（卡片上的状态点）。
	SetProviderStatus(ctx context.Context, req *aidto.SetStatusReq) (*aidto.Provider, error)
	// SaveModels 整组保存模型目录。
	SaveModels(ctx context.Context, req *aidto.SaveModelsReq) (*aidto.Provider, error)
	// RestoreDefaultModels 用代码内置清单整组替换模型目录；没有内置清单时报错不静默清空。
	RestoreDefaultModels(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.Provider, error)
	// FetchAvailableModels 调 {base_url}/models 拉取候选并合并进目录（走 SSRF 防护口径）。
	FetchAvailableModels(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.FetchModelsResult, error)
	// FetchModelCandidates 调 {base_url}/models 只拉候选、**不落库**（页面弹窗勾选后再写）。
	// 与 FetchAvailableModels 共用同一条地址解析 / SSRF / 错误归口路径，差别只在写不写库。
	FetchModelCandidates(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.ModelCandidates, error)
	// Chat 一次最小对话：按 provider_key 取一个已启用的供应商，用它的协议打一次上游并回文本。
	// 密钥口径同前：明文只在 service 内部解密后进请求头，返回值与响应体都不含密钥。
	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
}
