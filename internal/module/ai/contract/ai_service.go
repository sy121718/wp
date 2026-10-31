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

	Chat(ctx context.Context, req *aidto.ChatReq) (*aidto.ChatResult, error)
}
