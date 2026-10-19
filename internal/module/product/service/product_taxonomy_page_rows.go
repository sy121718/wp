package productservice

// product_taxonomy_page_rows.go — 分类树与品牌页的成品行（纯装配，形状只写一份）。
//
// 行元素是 dto 结构体；抽屉初值是 map（include 的数据根，取词函数由渲染层递归注入）。
// 控制器只做「绑定 → 调读模型 → 出口」，装配不再写在入口里。

import (
	productdto "go_wp/internal/module/product/dto"
	"strings"
)

// CategoryOptionIDs 父级候选下拉里出现过的分类 id（MissingParentIDs 的「已知集合」）。
func CategoryOptionIDs(options []productdto.PickOptionResp) map[string]struct{} {
	known := make(map[string]struct{}, len(options))
	for _, option := range options {
		if id := option.Value; id != "" {
			known[id] = struct{}{}
		}
	}
	return known
}

// CategoryDrawerForm 分类抽屉的片段数据（create 时 row 为 nil）。
//
// ParentMissing / ParentLabel / ParentOutOfScope 只在「父级不在候选列表」时给：模板写
// isset(.ParentLabel) 才用它，否则退回显示裸 ID —— 显示裸 ID 也比编一个父级名安全。
func CategoryDrawerForm(mode, projectID string, row *productdto.CategoryResp, options []productdto.PickOptionResp, parentLabels map[string]productdto.CategoryParentInfoResp) map[string]any {
	data := map[string]any{"Mode": mode, "Project": projectID, "Options": options}
	if row == nil {
		// 新建抽屉：每个键都给零值（模板直接读它们，缺键会让 Jets 从那行截断整个片段）。
		data["ID"], data["Name"], data["Slug"], data["ParentID"] = "", "", "", ""
		data["Sort"], data["Image"], data["Description"] = 0, "", ""
		data["SEOTitle"], data["SEODescription"] = "", ""
		return data
	}
	data["ID"], data["Name"], data["Slug"], data["ParentID"] = row.ID, row.Name, row.Slug, row.ParentID
	data["Sort"], data["Image"], data["Description"] = row.Sort, row.Image, row.Description
	data["SEOTitle"], data["SEODescription"] = row.SEOTitle, row.SEODescription
	if row.ParentID == "" {
		return data
	}
	parentMissing := true
	for _, option := range options {
		if option.Value == row.ParentID {
			parentMissing = false
			break
		}
	}
	data["ParentMissing"] = parentMissing
	if !parentMissing {
		return data
	}
	if info, ok := parentLabels[row.ParentID]; ok && strings.TrimSpace(info.Name) != "" {
		data["ParentLabel"] = info.Name
		data["ParentOutOfScope"] = info.OutOfScope
	}
	return data
}

// CategoryTreeRows 分类树 → 列表行；每行带自己的编辑抽屉。
//
// ParentOutOfScope 是行上的徽章：父级属于别的工程（或父级行已不存在）时置真。判据用
// MissingParentIDs（父级不在候选列表）而**不是** parentLabels —— 后者只收「名称读得到」的
// 那些父级，跨工程父级读不到名称会被它漏掉，而那时恰恰最该让操作者看见异常。
func CategoryTreeRows(projectID string, nodes []*productdto.CategoryResp, options []productdto.PickOptionResp, searching bool, parentLabels map[string]productdto.CategoryParentInfoResp) []productdto.CategoryRowResp {
	outOfScope := make(map[string]struct{})
	for _, parentID := range MissingParentIDs(nodes, CategoryOptionIDs(options)) {
		outOfScope[parentID] = struct{}{}
	}
	rows := make([]productdto.CategoryRowResp, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		_, flagged := outOfScope[node.ParentID]
		rows = append(rows, productdto.CategoryRowResp{
			ID: node.ID, Name: node.Name, Slug: node.Slug, Label: node.Name,
			ParentID: node.ParentID, Sort: node.Sort, Depth: node.Depth,
			HasChildren: node.HasChildren, Matched: node.Matched,
			SearchMode: searching, ParentOutOfScope: flagged,
			EditForm: CategoryDrawerForm("update", projectID, node, options, parentLabels),
		})
	}
	return rows
}

// BrandPageRows 当页品牌 → 成品行。
func BrandPageRows(details []*productdto.BrandResp) []productdto.BrandRowResp {
	rows := make([]productdto.BrandRowResp, 0, len(details))
	for _, b := range details {
		if b == nil {
			continue
		}
		rows = append(rows, productdto.BrandRowResp{
			ID: b.ID, Name: b.Name, Slug: b.Slug, Logo: b.Logo,
			Sort: b.Sort, Description: b.Description,
			UpdatedAt: b.UpdatedAt.TimeOrZero(),
		})
	}
	return rows
}
