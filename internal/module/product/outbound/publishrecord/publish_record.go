// Package publishrecord 自动发布模块的「当前发布登记」→ 商品发布记录页行的适配器（B9）。
//
// 位置判据（internal/module/CLAUDE.md §outbound）：这里**要翻译** —— presentation 的
// PublishRecordRow（实例 + 语言账本 + 产物版本）与这一屏要的形状（商品名 / 已上线与否 /
// 发布时间 / 版本号）不是一回事，且这一屏只取其中几个字段。
//
// 依赖方向：本包 → presentation 的 **contract**，不认识它的 service / model / enums。
package publishrecord

import (
	"context"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// Port 把发布登记翻成页面行（商品侧只做「按商品 id 合并」这一件业务事）。
type Port struct {
	records presentationcontract.PublishRecordPort
}

// New 构造。records 为 nil 时返回 nil（调用方据此把整页标成 Ready=false）。
func New(records presentationcontract.PublishRecordPort) *Port {
	if records == nil {
		return nil
	}
	return &Port{records: records}
}

// Langs 按商品 id 取发布登记，翻成「商品 → 逐语言明细」。
//
// entityIDs 为空 = 本工程全部商品类型实例。读失败直接上抛：这一屏的每一行都是发布事实，
// 静默返回空表会让运营以为「一个都没发布」。
func (p *Port) Langs(ctx context.Context, projectID string, entityIDs []string) (map[string][]productdto.PublishRecordLangResp, error) {
	if p == nil {
		return map[string][]productdto.PublishRecordLangResp{}, nil
	}
	rows, err := p.records.PublishRecords(ctx, projectID, productcontract.EntityTypeProduct, entityIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]productdto.PublishRecordLangResp, len(rows))
	for _, r := range rows {
		out[r.EntityID] = append(out[r.EntityID], productdto.PublishRecordLangResp{
			Lang:            r.Lang,
			OnlinePath:      r.ActivePath,
			Online:          r.Active,
			Stale:           r.Stale,
			PublishedAt:     r.PublishedAt,
			ArtifactVersion: r.ArtifactVersion,
			ArtifactHash:    r.ArtifactHash,
		})
	}
	return out, nil
}
