// Package publishrecord 自动发布模块的只读出口 → 商品页面的适配器（B9 / 迁移 671）。
//
// 两个能力：PublishRecordPort（现在发布成什么样）与 PublishAttemptPort（都试过哪几次）。
//
// 位置判据（internal/module/CLAUDE.md §outbound）：这里**要翻译** —— presentation 的
// PublishRecordRow（实例 + 语言账本 + 产物版本）与这一屏要的形状（商品名 / 已上线与否 /
// 发布时间 / 版本号）不是一回事，且这一屏只取其中几个字段。
//
// 依赖方向：本包 → presentation 的 **contract**，不认识它的 service / model / enums。
package publishrecord

import (
	"context"
	"strconv"
	"strings"

	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// Port 把发布登记与发布台账翻成页面形状（商品侧只做「按商品 id 合并」这一件业务事）。
type Port struct {
	records  presentationcontract.PublishRecordPort
	attempts presentationcontract.PublishAttemptPort
}

// New 构造。records 为 nil 时返回 nil（调用方据此把整页标成 Ready=false）。
func New(records presentationcontract.PublishRecordPort) *Port {
	if records == nil {
		return nil
	}
	return &Port{records: records}
}

// NewWithAttempts 构造带发布台账读口的适配器。
//
// 两条能力分开构造：发布记录页只要前者，发布历史面板只要后者；任一条未装配时，
// 缺的那一屏自己给「未装配」提示，而不是让整页 Ready=false。
func NewWithAttempts(records presentationcontract.PublishRecordPort,
	attempts presentationcontract.PublishAttemptPort) *Port {
	p := New(records)
	if p == nil && attempts == nil {
		return nil
	}
	if p == nil {
		p = &Port{}
	}
	p.attempts = attempts
	return p
}

// PublishAttempts 读某商品最近的发布尝试，翻成 JSON 出口形状。
//
// 与 Langs 同一取舍：读失败直接上抛。**未装配时也上抛** —— 面板要能说清
// 「发布历史没接上」与「这个商品没发布过」的区别，静默返回空列表会把两者混成一句。
func (p *Port) PublishAttempts(ctx context.Context, projectID, productID string, limit int) (*productdto.PublishAttemptListResp, error) {
	if p == nil || p.attempts == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	page, err := p.attempts.PublishAttempts(ctx, projectID, productcontract.EntityTypeProduct, productID, limit)
	if err != nil {
		return nil, err
	}
	res := &productdto.PublishAttemptListResp{
		Rows:    make([]productdto.PublishAttemptRowResp, 0, len(page.Rows)),
		Total:   page.Total,
		HasMore: page.HasMore,
	}
	for _, r := range page.Rows {
		res.Rows = append(res.Rows, productdto.PublishAttemptRowResp{
			ID:              r.ID,
			CreatedAtText:   r.CreatedAt.Local().Format("2006-01-02 15:04"),
			Trigger:         r.Trigger,
			Outcome:         r.Outcome,
			ErrorText:       r.ErrorText,
			Langs:           r.Langs,
			LangsText:       strings.Join(r.Langs, ", "),
			Attempts:        r.Attempts,
			AttemptsText:    attemptsText(r.Attempts),
			DurationText:    durationText(r.DurationMs),
			ArtifactVersion: r.ArtifactVersion,
			ArtifactHash:    r.ArtifactHash,
			OperatorName:    r.OperatorName,
			DurationMs:      r.DurationMs,
		})
	}
	return res, nil
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

// attemptsText 重试次数：一次成就不显示（模板据此留空）。
//
// 「重试」不是"第几次发布"而是"编译结果被丢弃重来"的次数 —— 有值才说明这一回不顺利。
func attemptsText(n int) string {
	if n <= 1 {
		return ""
	}
	return strconv.Itoa(n) + "x"
}

// durationText 耗时显示：<1s 给毫秒，其余给一位小数的秒。
func durationText(ms int64) string {
	if ms <= 0 {
		return ""
	}
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) + "s"
}
