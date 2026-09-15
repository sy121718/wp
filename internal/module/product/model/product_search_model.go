// product_search_model.go — 商品检索的仓储方法（BIZ-2 站内搜索）。
//
// 与 List 的区别：那个是后台列表（可筛任意状态、按关键词模糊、要总数），
// 本方法是**访问面**搜索 —— 只出已上架商品、限量、只取展示需要的那几列。
// 两条分开写而不是给 List 加参数：List 的调用方是后台（有权限），
// 检索的调用方是 anonymous 片段，「只出已上架」这条约束必须钉在语句里，
// 而不是靠调用方记得传 status=published。
package productmodel

import (
	"context"
	"strings"

	"go_wp/pkg/database"
)

// maxProductSearchLimit 单次检索的硬上限（访问面 anonymous 请求，不能退化成全表扫描）。
const maxProductSearchLimit = 50

// SearchPublished 按关键词检索工程内**已上架**商品的名称与副标题，只读、限量。
//
// 三条约束与内容侧同源：
//
//  1. **全参数化**：关键词只经占位符传递；
//  2. **LIKE 通配符转义**（ESCAPE '\'）：关键词里的 % 与 _ 是字面量 ——
//     不转义时搜「50%」会变成「以 50 开头」、搜「a_b」会命中「axb」，
//     用户以为搜到了，其实是搜索按另一套规则在工作；
//  3. **只出已上架**：status 条件写死在语句里（草稿商品不该出现在站内搜索结果里，
//     而搜索结果是可以被任何人构造并观察的）。
//
// 取列显式列出：description 是 JSONB 大字段，搜索结果不渲染全文，拉回来只是浪费带宽。
func (m *Model) SearchPublished(ctx context.Context, projectID, keyword string, limit int) (list []*ProductEntity, err error) {
	projectID = strings.TrimSpace(projectID)
	keyword = strings.TrimSpace(keyword)
	if projectID == "" || keyword == "" {
		return nil, nil
	}
	if limit <= 0 || limit > maxProductSearchLimit {
		limit = maxProductSearchLimit
	}
	pattern := "%" + database.EscapeLikePattern(keyword) + "%"
	err = m.db.WithContext(ctx).
		Select("id, project_id, name, subtitle, slug, status, default_image, update_time").
		Where("project_id = ?", projectID).
		Where("status = ?", productStatusPublished).
		Where("(name ILIKE ? ESCAPE '\\' OR subtitle ILIKE ? ESCAPE '\\')", pattern, pattern).
		Order("update_time DESC, id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// productStatusPublished 已上架状态值（与 product/enums.StatusPublished 同一字面量）。
//
// 在 model 里写一份字面量而不是 import enums：model 层的查询常量与 DDL 值同源，
// 引一层包只为拿一个字符串会让「状态值改了什么」这件事更难追。
const productStatusPublished = "published"
