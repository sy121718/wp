package mediaservice

// 为什么必须做：变体文件名带内容指纹 + /storage 对指纹名给 immutable 长缓存，
// 于是同一 URL 的字节在构造上不可变。代价是「内容变了」这件事必须有人通知引用方 ——
// 换图产出的是**一组新文件名**，已发布产物里的 srcset 仍指向旧名，而旧文件按设计保留
//（见 media_replace.go），旧 URL 继续返回旧字节。访客端 srcset 按 sizes 选中的多数是
// 变体而不是 src，所以「换图后新图不可见」不是缓存 bug，是这条义务没人承担。
//
// 引用集来自本模块的 extra_info.refs（构建期由引用方自己同步，见 media_ref.go）：
// 它记录的是**产物事实** —— 页面文档里写的 URL 未必进产物（条件渲染 / 块内联 /
// CMS 集合展开），只有编译后的 HTML 才是权威引用集。
//
// 失败处理：不吞。换图已经落盘、元数据已经回写，但「引用方不会更新」是用户可感知的
// 错误状态（他换的图在页面上永远不可见），所以失败**返回给调用方**并由 Replace 转成
// 业务错误透出。同时 Replace 的幂等分支（内容未变）也会走一次本函数 ——
// 于是「重传同一个文件」就是这条失败路径的自愈动作。

import (
	"context"
	"errors"
	"strings"

	"go_wp/internal/module/media/contract"
	"go_wp/internal/module/media/model"
	"go_wp/pkg/logger"
)

var _ mediacontract.MediaService = (*Service)(nil)

// Service 媒体模块业务逻辑。
type Service struct {
	am *mediamodel.AttachmentModel
	cm *mediamodel.FileCategoryModel
	vm *mediamodel.MediaVariantModel
	// staleMarkers 换图失效通知端口（kind → 实现）。装配期经 SetStaleMarkers 一次性
	// 注入，之后只读；nil 表示未注入（见 SetStaleMarkers 的 fail-fast）。
	staleMarkers map[string]mediacontract.StaleMarker
}

// NewService 创建媒体服务（am/cm/vm 分别为附件、分类、变体表访问单元）。
func NewService(am *mediamodel.AttachmentModel, cm *mediamodel.FileCategoryModel, vm *mediamodel.MediaVariantModel) *Service {
	return &Service{am: am, cm: cm, vm: vm}
}

// SetStaleMarkers 注入引用方失效通知实现（装配期调用一次，见 mediacontract.StaleMarker）。
//
// 每一个 RefKind* 都必须有认领者，缺一个就**当场失败**：少一类的表现是「换图后那一类
// 已发布页面永远停在旧字节」—— 内容确实换了、文件也在，只是没人告诉引用方，
// 日志里什么都没有。这与「端口未注入」是同一后果，因此同一判据。
//
// 装配层另有 wiring 清单的 required-port 条目兜底（未接线时启动自检报出端口与后果）；
// 这里先炸是为了连「标记了端口名、实际没注入」这种接线代码写错也拦住。
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

// notifyRefsStale 把「引用了该附件」的引用方标记为待重建。
//
// 返回 error 表示**至少有一类引用方没通知成功**（全部成功或本来就没有引用方时返回 nil）。
// 单类失败不中断其余类：一条脏引用记录不该让别的引用方也跟着停在旧字节。
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
	// 第三道防线（前两道：wiring 的 required-port 自检、SetStaleMarkers 的 kind 覆盖校验）：
	// 未注入实现时不静默跳过 —— 那会让「换图成功但引用方永不更新」看起来一切正常。
	if len(s.staleMarkers) == 0 {
		return errors.New("媒体换图失效通知端口未注入（装配缺陷）：引用方不会更新")
	}

	var firstErr error
	for kind, group := range grouped {
		marker := s.staleMarkers[kind]
		if marker == nil {
			// 装配期已校验每个 RefKind* 都有认领者，走到这里说明数据里的 kind 是
			// 本模块不认识的值（历史脏数据 / 手工写进 extra_info 的 JSON）。
			// 记 Error 并继续处理其余 kind —— 脏数据不该让别的引用方也不更新。
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
