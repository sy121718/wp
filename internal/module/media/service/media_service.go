package mediaservice

import (
	"context"
	"errors"
	"strings"

	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
)

var _ mediacontract.MediaService = (*Service)(nil)

type Service struct {
	am *mediamodel.AttachmentModel
	cm *mediamodel.FileCategoryModel
	vm *mediamodel.MediaVariantModel

	staleMarkers map[string]mediacontract.StaleMarker
}

func NewService(am *mediamodel.AttachmentModel, cm *mediamodel.FileCategoryModel, vm *mediamodel.MediaVariantModel) *Service {
	return &Service{am: am, cm: cm, vm: vm}
}

func (s *Service) SetStaleMarkers(markers ...mediacontract.StaleMarker) {
	if s == nil {
		return
	}
	registered := map[string]mediacontract.StaleMarker{}
	claimed := map[string]bool{}
	for _, m := range markers {
		if m == nil {
			continue
		}
		for _, kind := range m.RefKinds() {
			kind = strings.TrimSpace(kind)
			if kind == "" {
				continue
			}
			registered[kind] = m
			claimed[kind] = true
		}
	}
	var missing []string
	for _, kind := range []string{mediacontract.RefKindPage, mediacontract.RefKindPresentation} {
		if !claimed[kind] {
			missing = append(missing, kind)
		}
	}
	if len(missing) > 0 {
		panic("装配自检失败：媒体换图失效通知端口未接全（缺 " + strings.Join(missing, "、") +
			"）\n  未接入的后果：换图后该类已发布页面永不更新" +
			"（产物 srcset 指向旧变体名，旧文件按设计保留且 immutable，访客永远看到旧图）；" +
			"见 internal/routers/wiring.go 的 wiringManifest")
	}
	s.staleMarkers = registered
}

func (s *Service) notifyRefsStale(ctx context.Context, attachmentID uint64) error {
	refs, err := s.am.ListRefs(ctx, attachmentID)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	grouped := make(map[string][]mediacontract.MediaRef, len(refs))
	for i := range refs {
		kind := strings.TrimSpace(refs[i].Kind)
		id := strings.TrimSpace(refs[i].ID)
		if kind == "" || id == "" {
			continue
		}
		grouped[kind] = append(grouped[kind], mediacontract.MediaRef{Kind: kind, ID: id})
	}
	if len(grouped) == 0 {
		return nil
	}

	if len(s.staleMarkers) == 0 {
		return errors.New("媒体换图失效通知端口未注入（装配缺陷）：引用方不会更新")
	}

	var firstErr error
	for kind, group := range grouped {
		marker := s.staleMarkers[kind]
		if marker == nil {

			logger.Scene("media").With("attachment_id", attachmentID).
				With("ref_kind", kind).With("refs", len(group)).
				Error(errors.New("no stale marker registered for ref kind"),
					"媒体引用类型没有失效通知实现（该类型引用方不会更新，请核对该 kind 是否为 RefKind* 常量）")
			continue
		}
		marked, merr := marker.MarkStaleByMediaRefs(ctx, group)
		if merr != nil {
			logger.Scene("media").With("attachment_id", attachmentID).
				With("ref_kind", kind).With("refs", len(group)).
				Error(merr, "换图后的引用方失效标记失败（该引用方仍会显示旧图）")
			if firstErr == nil {
				firstErr = merr
			}
			continue
		}
		logger.Scene("media").With("attachment_id", attachmentID).
			With("ref_kind", kind).With("refs", len(group)).With("affected", len(marked)).
			Info("换图已标记引用方待重建")
	}
	return firstErr
}
