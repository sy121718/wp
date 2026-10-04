// ai_model_candidates.go — 「获取可用模型」的候选拉取（只读，不落库）。
//
// 页面上的「获取可用模型」是**两段动作**：先把候选拿出来给人看（本文件），
// 人勾完再写库（inbound 侧组装 SaveModelsReq）。拆开的原因是可撤销性 ——
// 旧行为是点一下就把整份 `/models` 并进目录，用户没有反悔的机会，
// 也没有机会把「这个中转站塞进来的一百个无关模型」挡在门外。
package aiservice

import (
	"context"
	"errors"

	aidto "go_wp/internal/module/ai/dto"
	aienums "go_wp/internal/module/ai/enums"
)

// FetchModelCandidates 拉 {base_url}/models 的候选列表并原样交回，**不写库**。
//
// 与 FetchAvailableModels 的唯一差别是写不写库：地址解析（自己填的 → 内置默认）、
// SSRF 校验、密钥解密、超时与错误归口都走同一个 probeModelIDs，
// 两条路径必须给出同样的「这家能不能拉」的判断，否则会出现「弹窗拉得到、确定后失败」。
func (s *Service) FetchModelCandidates(ctx context.Context, req *aidto.ProviderActionReq) (res *aidto.ModelCandidates, err error) {
	if req == nil || req.ProviderID <= 0 {
		return nil, errors.New(aienums.ErrInvalidParam)
	}
	current, err := s.m.FindByID(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrProviderNotFound
	}
	ids, perr := s.probeModelIDs(ctx, current)
	if perr != nil {
		return nil, perr
	}
	provider, err := s.GetProvider(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}
	existing := make([]string, 0, len(provider.Models))
	for i := range provider.Models {
		existing = append(existing, provider.Models[i].ID)
	}
	return &aidto.ModelCandidates{
		Provider:   provider,
		Candidates: ids,
		Existing:   existing,
		Fetched:    len(ids),
	}, nil
}
