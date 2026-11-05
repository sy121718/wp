package aicontract

import (
	"context"

	"go_wp/internal/module/ai/dto"
)

type AIService interface {
	ListProviders(ctx context.Context) ([]aidto.Provider, error)

	GetProvider(ctx context.Context, id int64) (*aidto.Provider, error)

	SaveProvider(ctx context.Context, req *aidto.SaveProviderReq) (*aidto.Provider, error)

	DeleteProvider(ctx context.Context, id int64) error

	SetProviderStatus(ctx context.Context, req *aidto.SetStatusReq) (*aidto.Provider, error)

	SaveModels(ctx context.Context, req *aidto.SaveModelsReq) (*aidto.Provider, error)

	RestoreDefaultModels(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.Provider, error)

	FetchAvailableModels(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.FetchModelsResult, error)

	FetchModelCandidates(ctx context.Context, req *aidto.ProviderActionReq) (*aidto.ModelCandidates, error)

	// AppendModels 把勾选的模型追加进目录并落库（去重 / 上限 / 乐观锁都在实现里）。
	// 与 SaveModels 分开是因为语义不同：保存是「以这份清单为准」，追加是「只增不删」。
	AppendModels(ctx context.Context, providerID, version, updateBy int64, selected []string) (*aidto.Provider, int, error)

	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
}
