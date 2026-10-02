package mediaservice

import (
	"strings"

	mediacontract "go_wp/internal/module/media/contract"
	mediamodel "go_wp/internal/module/media/model"
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
