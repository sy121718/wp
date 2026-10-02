package presentationservice

// presentation_media_stale.go — presentation 侧实现 media 索要的换图失效通知端口
//（mediacontract.StaleMarker，见 media/contract/media_service.go 的「索要的端口」）。
//
// 详情页产物同样引用媒体（商品图经构建期 srcset 烘进字节），而变体文件名带内容指纹：
// 换图产出新名、旧文件按设计保留，所以「谁引用了我」必须有人通知。
// presentation 侧的引用登记（refs 的 kind = presentation）当前**尚未接入**构建链
//（本模块的发布路径没有调用 media.SyncReferencesFromHTML，见报告「与描述不符的事实」），
// 因此本实现目前不会被触发；先把端口与实现接上，是为了让那条边一旦有引用集就自动生效，
// 而不是到时候再补一次接线（漏接的表现同样是「换图后详情页永远停在旧图」）。
//
// 放在 service 同包的理由与 page 侧一致：用的是本模块自己的 model，没有形状翻译。

import (
	"context"
	"errors"
	"strings"
	"time"

	mediacontract "go_wp/internal/module/media/contract"
	presentationenums "go_wp/internal/module/presentation/enums"
	"go_wp/pkg/logger"
)

var _ mediacontract.StaleMarker = (*Service)(nil)

// RefKinds 本实现认领的引用方类型：媒体引用集里 kind = presentation 的那些。
func (s *Service) RefKinds() []string { return []string{mediacontract.RefKindPresentation} }

// MarkStaleByMediaRefs 把「产物里引用了这张图」的自动发布实例标记为待重建。
//
// 逐工程独立作用域执行（DB-009 第二批，与 MarkStaleByRegistryVersion 同一手法）：
// presentation_instances 带 FORCE 策略，未设 app.project_id 的 UPDATE 在非超级角色下
// 静默 0 行 —— 表现是「换图后详情页不更新」且日志里没有任何异常，与端口未接入同形。
//
// 返回 RETURNING id 回读集合（真正命中的实例），不是入参回显；失败即失败，不吞。
func (s *Service) MarkStaleByMediaRefs(ctx context.Context, refs []mediacontract.MediaRef) (marked []string, err error) {
	if s == nil || s.m == nil {
		return nil, errors.New(presentationenums.ErrProjectRequired)
	}
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for i := range refs {
		if strings.TrimSpace(refs[i].Kind) != mediacontract.RefKindPresentation {
			continue
		}
		id := strings.TrimSpace(refs[i].ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if s.project == nil {
		return nil, errors.New(presentationenums.ErrProjectRequired)
	}
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now().UTC()
	markedSeen := map[string]bool{}
	for _, p := range projects {
		if ctx.Err() != nil {
			return marked, ctx.Err()
		}
		hit, merr := s.m.MarkStaleByIDs(ctx, p.ID, ids, at)
		if merr != nil {
			return nil, merr
		}
		for _, id := range hit {
			if id == "" || markedSeen[id] {
				continue
			}
			markedSeen[id] = true
			marked = append(marked, id)
		}
	}
	logger.Scene("presentation").With("affected", len(marked)).
		With("refs", len(ids)).Info("换图已标记自动发布实例待重建")
	return marked, nil
}
