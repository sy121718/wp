package blockservice

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockenums "go_wp/internal/module/block/enums"
	json "go_wp/pkg/jsonx"
)

func (s *Service) SetPresetResolver(resolver blockcontract.PresetResolver) {
	s.presetResolver = resolver
}

func (s *Service) resolveCreateDocument(ctx context.Context, req *blockdto.CreateReq) (json.RawMessage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if req.PresetKey == "" {
		return validateDocument(req.Document)
	}
	if s.presetResolver == nil {
		return nil, blockenums.Err(blockenums.ErrBlockPresetUnavailable)
	}
	document, err := s.presetResolver.ResolvePreset(ctx, req.PresetKey, req.PresetVersion)
	if err != nil {
		return nil, err
	}
	if len(document) == 0 {
		return nil, blockenums.Err(blockenums.ErrBlockPresetUnavailable)
	}
	return validateDocument(document)
}
