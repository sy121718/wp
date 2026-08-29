// contract_resolver.go — 媒体模块契约 → core.MediaResolver 适配。
// 让预览与正式构建链路把媒体库附件（DB）当作构建期媒体源：
// assetId（数字附件 ID）→ Detail → 稳定 URL 与基础元数据。
// 媒体库暂无多尺寸变体生成，variant 暂按 original 直出（接口保持，后端补变体后自动生效）。
package media

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"go_wp/internal/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	mediadto "go_wp/internal/module/media/dto"
)

// ContractResolver 基于 media 模块契约的构建期媒体解析器。
type ContractResolver struct {
	Svc mediacontract.MediaService
}

// NewContractResolver 创建适配器。
func NewContractResolver(svc mediacontract.MediaService) *ContractResolver {
	return &ContractResolver{Svc: svc}
}

// ResolveMedia 实现 core.MediaResolver：附件 ID → URL 与元数据。
func (r *ContractResolver) ResolveMedia(assetID, variant string) (meta *core.MediaMeta, err error) {
	if r.Svc == nil {
		return nil, fmt.Errorf("媒体解析器未装配")
	}
	id := strings.TrimSpace(assetID)
	attachmentID, parseErr := strconv.ParseUint(id, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("附件 ID 不合法: %q", assetID)
	}
	att, err := r.Svc.Detail(context.Background(), &mediadto.DetailReq{ID: attachmentID})
	if err != nil {
		return nil, fmt.Errorf("附件 %d 查询失败: %w", attachmentID, err)
	}
	if att == nil || att.URL == "" {
		return nil, fmt.Errorf("附件 %d 不存在或缺少 URL", attachmentID)
	}
	name := att.FileName
	return &core.MediaMeta{
		Type:     core.MediaTypeImage,
		MimeType: att.MimeType,
		URL:      att.URL,
		Alt:      name,
		Title:    name,
	}, nil
}
