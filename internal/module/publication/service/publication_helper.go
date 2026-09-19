package pubservice

// publication_helper.go — 路径归一化与路由行映射（归属者判定、响应组装）。

import (
	"errors"
	"strings"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/pathkit"
	"go_wp/pkg/utils"
)

// normalizePath 规范化路由路径：路由占用（预留 / 改名 / 激活）之前的统一入口口径。
//
// 归一化规则唯一实现在 pkg/pathkit.NormalizeRoutePath（审计 CQ-012）：这里过去自己
// 实现了一遍「循环去尾斜杠 + 拒绝部分字符」，与 pipeline.NormalizeURL 有两处**真实
// 分歧**（对照测试见 path_normalize_contract_test.go）——
//   - "/a//b" 被原样接受：同一路径的两种写法可以各占一行路由，占用判断失真；
//   - "/index" 与 "/index.html" 不归一为根路径：与首页路由并存时线上内容与路由记录分裂。
//
// 现在统一到同一实现；非法输入仍属于参数格式错误，返回 ErrInvalidParam
// （与资源占用语义区分）。
func normalizePath(raw string) (string, error) {
	p, err := pathkit.NormalizeRoutePath(raw)
	if err != nil {
		return "", errors.New(pubenums.ErrInvalidParam)
	}
	return p, nil
}

func strPtr(s string) *string { return &s }

func routeResp(e *pubmodel.RouteEntity) *pubdto.RouteResp {
	return &pubdto.RouteResp{
		ProjectID: e.ProjectID, Path: e.Path, PageID: e.PageID,
		PresentationID: e.PresentationID,
		RouteKind:      e.RouteKind, ArtifactID: e.ArtifactID, UpdatedAt: utils.NewJSONTime(e.UpdatedAt),
	}
}

// routeOwner 路由行归属者：page_routes 的 CHECK 约束要求 page_id 与
// presentation_id 恰好一个非空（见 init_builder_schema.sql），所以归属者
// 用「二选一」表达，而不是两个可空参数——后者会让「两个都传」变成一行
// 违反 CHECK 的写入，报错点落在数据库而不是参数校验。
type routeOwner struct {
	pageID         string
	presentationID string
}

// parseRouteOwner 解析归属者并校验恰好一个非空。
func parseRouteOwner(pageID, presentationID string) (routeOwner, error) {
	o := routeOwner{
		pageID:         strings.TrimSpace(pageID),
		presentationID: strings.TrimSpace(presentationID),
	}
	if (o.pageID == "") == (o.presentationID == "") {
		return routeOwner{}, errors.New(pubenums.ErrInvalidParam)
	}
	return o, nil
}

// sourceType 回执来源类型（publication_receipts.source_type）。
//
// 字面量取 pubdto 的常量而不是就地写：page / presentation 两侧的收敛例程按这个值
// 筛选「自己该管的那一批回执」，各写一遍字面量迟早分叉，而分叉的表现是
// 「回执留在 pending 但没有任何收敛例程认领它」。
func (o routeOwner) sourceType() string {
	if o.presentationID != "" {
		return pubdto.ReceiptSourcePresentation
	}
	return pubdto.ReceiptSourcePage
}

// sourceID 回执来源 id（source_id 是 uuid NOT NULL 列，必须写归属者本身）。
func (o routeOwner) sourceID() string {
	if o.presentationID != "" {
		return o.presentationID
	}
	return o.pageID
}

// pageIDPtr 页面侧归属列：展示实例归属时必须留 NULL，否则违反 CHECK。
func (o routeOwner) pageIDPtr() *string {
	if o.pageID == "" {
		return nil
	}
	return strPtr(o.pageID)
}

// presentationIDPtr 展示实例侧归属列（页面归属时留 NULL）。
func (o routeOwner) presentationIDPtr() *string {
	if o.presentationID == "" {
		return nil
	}
	return strPtr(o.presentationID)
}

// match 归属者匹配条件（UPDATE / DELETE 精确定位本归属者的行）。
func (o routeOwner) match() (string, []any) {
	if o.presentationID != "" {
		return "presentation_id = ?", []any{o.presentationID}
	}
	return "page_id = ?", []any{o.pageID}
}

// ON CONFLICT DO UPDATE 用的归属者一致性判定（ownershipExprSQL）已下移到 model：
// 见 pubmodel.ActivateRouteTx —— 它是一段 SQL 片段，与使用它的 upsert 语句同处一地。
