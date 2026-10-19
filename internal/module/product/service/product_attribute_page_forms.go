package productservice

// product_attribute_page_forms.go — 属性库页的抽屉表单初值与成品行（纯装配，形状只写一份）。
//
// 这里的函数只产「模板键 → 值」：取词函数（t/tf/tn）与 csrf_token 由渲染层注入
//（internal/text.Inject 会递归补给每一张 map），所以装配不需要 gin.Context ——
// 控制器因此只剩「绑定 → 调读模型 → 出口」。

import (
	"strconv"
	"strings"

	productdto "go_wp/internal/module/product/dto"
)

// 属性组表单的两种形态：新建（页头 / 空态打开的抽屉）与编辑（列表每行一个抽屉）。
const (
	AttrGroupModeCreate = "create"
	AttrGroupModeEdit   = "edit"
)

// AttrRowsFromValues 属性值列表 → 表单行（索引按位置给，排序值转成文本）。
func AttrRowsFromValues(rows []productdto.AttributeValueResp) []productdto.AttrValueFormRow {
	out := make([]productdto.AttrValueFormRow, 0, len(rows))
	for i, row := range rows {
		out = append(out, productdto.AttrValueFormRow{
			Index: i, ID: row.ID, Key: row.Key, Label: row.Label,
			Sort: strconv.Itoa(row.Sort), Enabled: row.Enabled,
		})
	}
	return out
}

// AttrRowsCtx 属性值编辑器片段的渲染数据（include 传 .RowsCtx）。
func AttrRowsCtx(groupID string, rows []productdto.AttrValueFormRow) map[string]any {
	return map[string]any{"GroupID": groupID, "Rows": rows}
}

// AttrGroupFormOpts 属性组抽屉表单的初值。
//
// 用结构体而不是位置参数：这里连着两个 id（工程 / 属性组）与一个模式串，位置写反
// （工程 id 塞进属性组槽）不会编译失败 —— 只会在抽屉里把工程 id 渲染进隐藏域。
type AttrGroupFormOpts struct {
	ProjectID string
	Mode      string
	GroupID   string
	Name      string
	Key       string
	Sort      int
	Variation bool
	RowsCtx   map[string]any
}

// AttrGroupForm 属性组抽屉表单的数据（结构性键恒给齐，片段在任何路径下都能独立成立）。
//
// 没有回填键：抽屉保存走 JSON 写基座，失败不再由服务端重渲染这一块。
func AttrGroupForm(o AttrGroupFormOpts) map[string]any {
	return map[string]any{
		"Project": o.ProjectID,
		"GroupID": o.GroupID, "IsCreate": o.Mode == AttrGroupModeCreate,
		"Name": o.Name, "Key": o.Key, "Sort": o.Sort,
		"VariationChecked": o.Variation,
		"RowsCtx":          o.RowsCtx,
	}
}

// AttrValuesForm 属性值抽屉表单的数据（可见字段全是 values[n].*，现值由 RowsCtx 带出）。
func AttrValuesForm(projectID, groupID string, rowsCtx map[string]any) map[string]any {
	return map[string]any{
		"Project": projectID,
		"GroupID": groupID,
		"RowsCtx": rowsCtx,
	}
}

// AttrCreateDrawerForm 「新建属性组」抽屉的初值（页头与空态两个入口共用同一份）。
//
// 首屏属性值行**为空**（nil）：编辑器渲染 0 行 + 引导文案，用户点「+ 添加值」后由片段兜底
// 保证至少一行 —— 别按「同口径」在这里补一行空输入。
func AttrCreateDrawerForm(projectID string) map[string]any {
	return AttrGroupForm(AttrGroupFormOpts{
		ProjectID: projectID,
		Mode:      AttrGroupModeCreate, GroupID: "new", Variation: true,
		RowsCtx: AttrRowsCtx("new", nil),
	})
}

// AttrRowDrawerForms 属性组列表里一行的两个抽屉初值（编辑组 / 编辑值）。
func AttrRowDrawerForms(projectID string, a *productdto.AttributeResp) (group, values map[string]any) {
	group = AttrGroupForm(AttrGroupFormOpts{
		ProjectID: projectID,
		Mode:      AttrGroupModeEdit, GroupID: a.ID,
		Name: a.Name, Key: a.Key, Sort: a.Sort, Variation: a.IsVariation,
	})
	values = AttrValuesForm(projectID, a.ID, AttrRowsCtx(a.ID, AttrRowsFromValues(a.Values)))
	return group, values
}

// AttrPageRows 当页属性组 → 成品行（每行带两个抽屉的初值）。
func AttrPageRows(projectID string, details []*productdto.AttributeResp) []productdto.AttributeRowResp {
	rows := make([]productdto.AttributeRowResp, 0, len(details))
	for _, a := range details {
		if a == nil {
			continue
		}
		editForm, valuesForm := AttrRowDrawerForms(projectID, a)
		rows = append(rows, productdto.AttributeRowResp{
			ID: a.ID, Key: a.Key, Name: a.Name,
			IsVariation: a.IsVariation, Sort: a.Sort, ValueCount: a.ValueCount,
			EditForm: editForm, ValuesForm: valuesForm,
		})
	}
	return rows
}

// AttrValueRows 值编辑器片段的数据根（add / remove 一次，回渲染整段行列表）。
//
// 编辑器只重建**原始表单值与位置**：落库时的归一 / 去重 / 排序仍由保存路径负责。
// 空列表兜一行 —— 没有行时「添加」按钮无从下手。
func AttrValueRows(req *productdto.AttributeRowsFormReq) map[string]any {
	rows := req.Rows
	switch strings.TrimSpace(req.Action) {
	case "add":
		index := 0
		if len(rows) > 0 {
			index = rows[len(rows)-1].Index + 1
		}
		rows = append(rows, productdto.AttrValueFormRow{Index: index, Enabled: true})
	case "remove":
		if idx := req.RemoveIndexValue(); idx >= 0 && idx < len(rows) {
			rows = append(rows[:idx], rows[idx+1:]...)
		}
	}
	if len(rows) == 0 {
		rows = []productdto.AttrValueFormRow{{Enabled: true}}
	}
	groupID := strings.TrimSpace(req.GroupID)
	if groupID == "" {
		groupID = "new"
	}
	return map[string]any{"GroupID": groupID, "Rows": rows}
}
