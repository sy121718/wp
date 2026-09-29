package adminhttp

// datarule_config_form.go — 数据规则配置的表单化编辑（替代裸 JSON textarea）。
//
// 表单约定（组序号 = g，行序 = 行在数组中的位置；同名 input 重复出现即被浏览器提交为数组）：
//
//	config_editor            隐藏标记：出现即表示本次提交带完整配置（否则沿用库中原配置）
//	omit_fields              多选：查询结果中屏蔽的字段（限于该域白名单）
//	groups[g].logic          条件组 g 的组合逻辑（AND / OR）
//	groups[g].conditions[i].field / .op / .value
//	                         条件组 g 的第 i 行（字段 / 操作符 / 值）
//
// 「添加 / 删除行与组、切换字段」都打回同一个片段端点：服务端从提交的表单值重建当前配置、
// 应用动作、再按该域白名单渲染整块编辑器。当前编辑到一半的内容始终由表单承载 ——
// 服务端没有会话状态，前端没有一行 JS（与 product_attribute_rows 同型）。
//
// 服务端只解析、不判断；字段是否属于该域、操作符是否被该字段声明，仍由 service 的
// validateRuleConfig 按域声明判定（表单下拉只是让人不容易填错，不是校验）。

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"

	admindto "go_wp/internal/module/admin/dto"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/internal/web/shell"
	"go_wp/pkg/datarule"

	"github.com/gin-gonic/gin"
)

const (
	// dataruleEditorMarker 编辑器隐藏标记字段名。
	dataruleEditorMarker = "config_editor"
	// dataruleMaxGroups / dataruleMaxRowsPerGroup 一组上限：规则是给人读的，
	// 条件铺到几十条时更该拆成多条规则（组间已经是 AND，拆开语义不变）。
	dataruleMaxGroups       = 8
	dataruleMaxRowsPerGroup = 12
)

// dataruleOption 一个下拉选项（值 + 显示文本 + 是否选中）。
type dataruleOption struct {
	Value    string
	Label    string
	Selected bool
}

// dataruleFieldView 屏蔽字段多选里的一项。
type dataruleFieldView struct {
	Field   string
	Label   string
	Omitted bool
}

// dataruleRowView 一个条件行。
type dataruleRowView struct {
	FieldOptions []dataruleOption
	OpOptions    []dataruleOption
	Value        string
}

// dataruleGroupView 一个条件组。
type dataruleGroupView struct {
	Index     int
	Logic     string
	Rows      []dataruleRowView
	CanAddRow bool
}

// dataruleEditorCtx 配置编辑器片段的数据。
type dataruleEditorCtx struct {
	Domain      string
	FieldCount  int
	Fields      []dataruleFieldView
	Groups      []dataruleGroupView
	CanAddGroup bool
}

// dataruleFormRowRe 解析 groups[g].conditions[i].{field,op,value}。
var dataruleFormRowRe = regexp.MustCompile(`^groups\[(\d+)\]\.conditions\[(\d+)\]\.(field|op|value)$`)

// dataruleFormLogicRe 解析 groups[g].logic。
var dataruleFormLogicRe = regexp.MustCompile(`^groups\[(\d+)\]\.logic$`)

// dataruleConfigFromForm 从表单重建规则配置（保留空组与空行，供编辑器回渲染）。
func dataruleConfigFromForm(c *gin.Context) admindto.RuleConfigDTO {
	// 自己保证表单已解析：本函数直接读 Request.PostForm，若指望调用方先摸过一次
	// c.PostForm，换个调用顺序（或换个入口）就会静默解析出空配置 —— 用户填的条件
	// 会全部消失，且没有任何报错。
	_ = c.Request.ParseForm()

	cfg := admindto.RuleConfigDTO{OmitFields: c.PostFormArray("omit_fields")}
	if cfg.OmitFields == nil {
		cfg.OmitFields = []string{}
	}

	// 组序与行序都由表单单键决定：先扫出所有出现过的 (g, i) 与 g.logic，
	// 再按升序填充，保证「删掉中间一行」之后剩下的行仍然按屏幕上的顺序落库。
	type rowKey struct{ group, row int }
	rows := map[rowKey]*admindto.RuleConditionDTO{}
	logics := map[int]string{}
	maxGroup, maxRow := -1, map[int]int{}
	for key, values := range c.Request.PostForm {
		value := ""
		if len(values) > 0 {
			value = values[0]
		}
		if m := dataruleFormRowRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			i, _ := strconv.Atoi(m[2])
			item, ok := rows[rowKey{g, i}]
			if !ok {
				item = &admindto.RuleConditionDTO{}
				rows[rowKey{g, i}] = item
			}
			switch m[3] {
			case "field":
				item.Field = strings.TrimSpace(value)
			case "op":
				item.Op = strings.TrimSpace(value)
			case "value":
				item.Value = strings.TrimSpace(value)
			}
			if g > maxGroup {
				maxGroup = g
			}
			if i+1 > maxRow[g] {
				maxRow[g] = i + 1
			}
			continue
		}
		if m := dataruleFormLogicRe.FindStringSubmatch(key); m != nil {
			g, _ := strconv.Atoi(m[1])
			logics[g] = strings.TrimSpace(value)
			if g > maxGroup {
				maxGroup = g
			}
		}
	}

	groups := make([]admindto.RuleConditionGroupDTO, 0, maxGroup+1)
	for g := 0; g <= maxGroup; g++ {
		group := admindto.RuleConditionGroupDTO{Logic: logics[g]}
		for i := 0; i < maxRow[g]; i++ {
			if item, ok := rows[rowKey{g, i}]; ok {
				group.Conditions = append(group.Conditions, *item)
			}
		}
		groups = append(groups, group)
	}
	cfg.ConditionGroups = groups
	return cfg
}

// dataruleUsableConditions 过滤掉「只有空壳」的条件行。
//
// 判据是**值也必须有**：引擎对空值会生成 field = ”（一条永远匹配不到的条件），
// 而空字段名会被直接丢弃 —— 两种都会让「看起来配了三条、实际只生效一条」。
// 落库前统一清掉，页面上的空行则原样保留给人继续填。
func dataruleUsableConditions(rows []admindto.RuleConditionDTO) []admindto.RuleConditionDTO {
	kept := make([]admindto.RuleConditionDTO, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Field) == "" || strings.TrimSpace(row.Value) == "" {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// dataruleDropEmptyGroups 丢弃没有任何可用条件的组。
func dataruleDropEmptyGroups(cfg admindto.RuleConfigDTO) admindto.RuleConfigDTO {
	groups := make([]admindto.RuleConditionGroupDTO, 0, len(cfg.ConditionGroups))
	for _, group := range cfg.ConditionGroups {
		if conditions := dataruleUsableConditions(group.Conditions); len(conditions) > 0 {
			group.Conditions = conditions
			groups = append(groups, group)
		}
	}
	cfg.ConditionGroups = groups
	return cfg
}

// dataruleEditorContext 组装编辑器片段数据：该域的白名单 + 当前配置的组/行视图。
func (h *AdminPagesHandle) dataruleEditorContext(c *gin.Context, domain string, cfg admindto.RuleConfigDTO) dataruleEditorCtx {
	// 取词函数：下拉里的空选项与提示由本文件拼进片段 HTML（模板层不参与这些句子），
	// 所以在这里取一次当前语言（迁移 452 seed 中英词条）。
	tr := shell.TranslateFor(c)
	ctx := dataruleEditorCtx{
		Domain:     domain,
		Fields:     []dataruleFieldView{},
		Groups:     []dataruleGroupView{},
		FieldCount: 0,
	}

	detail, _ := h.rules.RuleSchemaDetail(c.Request.Context(), &admindto.RuleSchemaDetailReq{Domain: domain})
	labels := map[string]string{}
	opsByField := map[string][]string{}
	order := make([]string, 0, 8)
	if detail != nil {
		for _, f := range detail.Fields {
			order = append(order, f.Field)
			labels[f.Field] = f.Label
			opsByField[f.Field] = f.Operators
		}
	}
	ctx.FieldCount = len(order)

	omitted := map[string]bool{}
	for _, f := range cfg.OmitFields {
		omitted[f] = true
	}
	for _, field := range order {
		ctx.Fields = append(ctx.Fields, dataruleFieldView{Field: field, Label: labels[field], Omitted: omitted[field]})
	}

	fieldOptions := func(selected string) []dataruleOption {
		options := make([]dataruleOption, 0, len(order)+1)
		options = append(options, dataruleOption{Value: "", Label: tr(adminenums.DatarulesEditorFieldPlaceholder, "请选择字段"), Selected: selected == ""})
		for _, field := range order {
			options = append(options, dataruleOption{
				Value:    field,
				Label:    labels[field] + "（" + field + "）",
				Selected: field == selected,
			})
		}
		return options
	}
	opOptions := func(field, selected string) []dataruleOption {
		ops := opsByField[field]
		if len(ops) == 0 {
			return []dataruleOption{{Value: "", Label: tr(adminenums.DatarulesEditorFieldRequired, "请先选择字段"), Selected: true}}
		}
		options := make([]dataruleOption, 0, len(ops))
		for _, op := range ops {
			options = append(options, dataruleOption{Value: op, Label: op, Selected: op == selected})
		}
		return options
	}

	groups := cfg.ConditionGroups
	if len(groups) > dataruleMaxGroups {
		groups = groups[:dataruleMaxGroups]
	}
	for gi, group := range groups {
		logic := datarule.LogicAnd
		if strings.EqualFold(strings.TrimSpace(group.Logic), datarule.LogicOr) {
			logic = datarule.LogicOr
		}
		view := dataruleGroupView{Index: gi, Logic: logic, CanAddRow: len(group.Conditions) < dataruleMaxRowsPerGroup}
		for _, row := range group.Conditions {
			view.Rows = append(view.Rows, dataruleRowView{
				FieldOptions: fieldOptions(row.Field),
				OpOptions:    opOptions(row.Field, row.Op),
				Value:        row.Value,
			})
		}
		ctx.Groups = append(ctx.Groups, view)
	}
	if len(ctx.Groups) == 0 {
		// 至少给一组一行可编辑（空列表会让「添加条件」无处落脚）。
		ctx.Groups = append(ctx.Groups, dataruleGroupView{
			Index:     0,
			Logic:     datarule.LogicAnd,
			Rows:      []dataruleRowView{{FieldOptions: fieldOptions(""), OpOptions: opOptions("", "")}},
			CanAddRow: true,
		})
	}
	ctx.CanAddGroup = len(ctx.Groups) < dataruleMaxGroups
	return ctx
}

// DataruleConfigEditor 配置编辑器片段（HTMX）：add_row / del_row / add_group / del_group / refresh。
//
// 它不落库、也不做业务校验 —— 只是把「当前表单里已有的配置」按该域白名单重新渲染一遍，
// 所以不挂 Casbin（与 product 的属性值行编辑器一致；真正的写入仍走 /admin/datarules/update）。
func (h *AdminPagesHandle) DataruleConfigEditor(c *gin.Context) {
	domain := shell.FieldValue(c, "domain")
	cfg := dataruleConfigFromForm(c)

	switch shell.FieldValue(c, "action") {
	case "add_group":
		if len(cfg.ConditionGroups) < dataruleMaxGroups {
			cfg.ConditionGroups = append(cfg.ConditionGroups, admindto.RuleConditionGroupDTO{Logic: datarule.LogicAnd})
		}
	case "del_group":
		gi := int(shell.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) {
			cfg.ConditionGroups = append(cfg.ConditionGroups[:gi], cfg.ConditionGroups[gi+1:]...)
		}
	case "add_row":
		gi := int(shell.ParseUint(c.PostForm("group")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && len(cfg.ConditionGroups[gi].Conditions) < dataruleMaxRowsPerGroup {
			cfg.ConditionGroups[gi].Conditions = append(cfg.ConditionGroups[gi].Conditions, admindto.RuleConditionDTO{})
		}
	case "del_row":
		gi := int(shell.ParseUint(c.PostForm("group")))
		ri := int(shell.ParseUint(c.PostForm("row")))
		if gi >= 0 && gi < len(cfg.ConditionGroups) && ri >= 0 && ri < len(cfg.ConditionGroups[gi].Conditions) {
			rows := cfg.ConditionGroups[gi].Conditions
			cfg.ConditionGroups[gi].Conditions = append(rows[:ri], rows[ri+1:]...)
		}
	}

	c.HTML(http.StatusOK, "admin/system/datarule_config_editor.html",
		shell.Prepare(c, gin.H{"Editor": h.dataruleEditorContext(c, domain, cfg)}))
}
