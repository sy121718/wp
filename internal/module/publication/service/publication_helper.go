package pubservice

// publication_helper.go — 路径归一化与路由行映射（归属者判定、响应组装）。

import (
	"errors"
	"strings"

	pubdto "go_wp/internal/module/publication/dto"
	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/pkg/pathkit"

	"gorm.io/gorm/clause"
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
		RouteKind:      e.RouteKind, ArtifactID: e.ArtifactID, UpdatedAt: e.UpdatedAt,
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
func (o routeOwner) sourceType() string {
	if o.presentationID != "" {
		return "presentation"
	}
	return "page"
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

// ownershipExpr ON CONFLICT DO UPDATE 的归属者一致性判定。
//
// 用 IS NOT DISTINCT FROM 而不是 = ：展示实例的行 page_id 为 NULL，而
// NULL = NULL 在 SQL 里求值为 NULL（不成立），按 page_id 比会让实例连
// 「重复激活自己」都失败（第二次发布会误判成 ErrRouteOccupied）。
// IS NOT DISTINCT FROM 把 NULL 当作可比较值，两类归属者都能正确判等。
func (o routeOwner) ownershipExpr() clause.Expression {
	return clause.Expr{SQL: "page_routes.page_id IS NOT DISTINCT FROM EXCLUDED.page_id" +
		" AND page_routes.presentation_id IS NOT DISTINCT FROM EXCLUDED.presentation_id"}
}
