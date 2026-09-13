// presentation_locator.go — 实现 contract.PublishedEntityLocator（BIZ-2 站内搜索）。
//
// 搜索片段拿到的只有实体 id：cms 内容与商品的「有没有线上页面」不在它们自己的表上，
// 而在这里（presentation_instances 的 active 指针）。这条收窄端口把那个事实读出来，
// 顺带给调用方一个**可用的路径** —— 没有它，搜索结果就只能是不带链接的一堆标题。
package presentationservice

import (
	"context"
	"strings"

	"github.com/google/uuid"

	presentationcontract "go_wp/internal/module/presentation/contract"
)

// 编译期断言：本 service 提供访问面解析线上路径需要的只读能力。
var _ presentationcontract.PublishedEntityLocator = (*Service)(nil)

// PublishedEntityPaths 按实体 id 批量解析已上线详情页的线上路径。
//
// 实现只做两件事：形状过滤（uuid）与查询下推；「已上线」的判定在 SQL 条件里
// （active_artifact_id IS NOT NULL），不在这里二次过滤。
func (s *Service) PublishedEntityPaths(ctx context.Context, projectID, entityType string, entityIDs []string) (map[string]string, error) {
	ids := normalizeLocatorIDs(entityIDs)
	if len(ids) == 0 {
		return map[string]string{}, nil
	}
	return s.m.ListActiveURLPaths(ctx, projectID, entityType, ids)
}

// normalizeEntityIDs 去空、去重、**形状过滤**（uuid），保持首次出现顺序。
//
// 形状过滤是必需的而不是防御性洁癖：实体 id 来自片段 URL（任何人可构造），
// 非 uuid 的字符串带进 entity_id（uuid 列）的比较会让 PostgreSQL 直接报
// invalid input syntax for type uuid（SQLSTATE 22P02）—— 一个搜索请求就能把页面打成 500。
// 非法 id 在这里被丢弃，调用方按「查不到」处理（与真的没发布同一条路）。
func normalizeLocatorIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, err := uuid.Parse(id); err != nil {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
