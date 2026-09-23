package templates

// product_attribute_form_fail_render_test.go — 属性页两个抽屉表单片段的渲染契约。
//
// 这两个片段（partials/product_attribute_group_form.html / product_attribute_values_form.html）
// 在**两条路径**上渲染：页面首屏（<template> 里的 include，初值 = 库里当前值）与提交失败时
// 由 handler 单独重渲染（值 = 用户刚填的内容）。三件事必须真的渲出来，否则表现为
// 「保存失败后回填没了 / 表单被整块替换掉 / 取消按钮消失」，而模板层没有任何报错：
//   - host 容器 + htmx 三分属性（hx-post / hx-target=closest / hx-swap=outerHTML）——
//     缺任何一个，htmx 就不会替换「那一个单元」，失败后页面半旧半新；
//   - 错误槽（role=alert）在**表单之外、host 之内** —— 放进表单里会被下一次提交一起换掉；
//   - 回填值真的落到 value / checked 上，且勾选态**按值命中**
//     （用「该字段提交过」判会被同名隐藏域骗到：未勾选的组回填后变成勾上的）。
//
// 首屏（无 FormEcho / SubmitErr）必须走另一条分支：初值取 .Name / .Key / .Sort /
// .VariationChecked，且**没有**错误槽 —— 这是 gfFill / vfFill 开关的契约。

import (
	"strings"
	"testing"
)

// attrRowsCtxData 属性值行编辑器的数据类（形状与 handle 的 attrRowsCtx 一致）。
func attrRowsCtxData(groupID string, rows []map[string]any) map[string]any {
	return map[string]any{"GroupID": groupID, "Rows": rows}
}

// attrGroupFragmentData 属性组片段的渲染数据（键与 handle 的 attrGroupFormData 逐键一致）。
//
// echo=false 是**首屏**：不注入 FormEcho* 三键（片段以 gfFill 门控走初值分支）；
// echo=true 是**失败重渲染**：三个回填键 + SubmitErr 都在场。
func attrGroupFragmentData(echo bool) map[string]any {
	rows := []map[string]any{}
	if echo {
		// 用户刚编的那一行：失败重渲染必须把它留住（行数据走 RowsCtx，不是单值回填）。
		rows = []map[string]any{{"ID": "", "Key": "red", "Label": "红", "Sort": 0, "Enabled": true}}
	}
	d := map[string]any{
		"Csrf": "tok", "Project": "pr1", "t": TranslateFunc("zh-CN"),
		"Mode": "create", "Action": "/admin/product-attributes/create", "IsCreate": true,
		"GroupID": "new", "Name": "", "Key": "", "Sort": 0, "VariationChecked": true,
		"RowsCtx": attrRowsCtxData("new", rows), "InDrawer": true,
	}
	if echo {
		d["SubmitErr"] = "属性组标识已被占用"
		d["FormEcho"] = map[string]any{
			"projectId": "pr1", "id": "new", "name": "颜色", "key": "color", "sort": "3", "isVariation": "0",
		}
		d["FormEchoChecked"] = map[string]any{"isVariation": true}
		d["FormEchoMulti"] = map[string]any{"isVariation": []string{"0", "1"}}
	}
	return d
}

// attrValuesFragmentData 属性值片段的渲染数据（键与 handle 的 attrValuesFormData 逐键一致）。
func attrValuesFragmentData(echo bool) map[string]any {
	rows := []map[string]any{}
	groupID := "a1"
	if echo {
		// 两行：第一行启用、第二行被用户明确取消勾选 —— 回填要把两个状态原样带回来。
		rows = []map[string]any{
			{"ID": "av1", "Key": "red", "Label": "红", "Sort": 0, "Enabled": true},
			{"ID": "av2", "Key": "blue", "Label": "蓝", "Sort": 0, "Enabled": false},
		}
	} else {
		rows = []map[string]any{{"ID": "av1", "Key": "red", "Label": "红", "Sort": 0, "Enabled": true}}
	}
	d := map[string]any{
		"Csrf": "tok", "Project": "pr1", "t": TranslateFunc("zh-CN"),
		"Action": "/admin/product-attributes/set-values", "GroupID": groupID,
		"RowsCtx": attrRowsCtxData(groupID, rows), "InDrawer": true,
	}
	if echo {
		d["SubmitErr"] = "属性值标识重复"
		d["FormEcho"] = map[string]any{"projectId": "pr1", "id": groupID, "groupId": groupID}
		d["FormEchoChecked"] = map[string]any{"id": true, "groupId": true}
		d["FormEchoMulti"] = map[string]any{"id": []string{groupID}, "groupId": []string{groupID}}
	}
	return d
}

// renderAttrFragment 直渲片段（失败重渲染走的就是这条渲染路径：无 layout，data 由 handler 给齐）。
func renderAttrFragment(t *testing.T, name string, data map[string]any) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/product/"+name, data)
	if err != nil {
		t.Fatalf("渲染片段 %s 失败: %v", name, err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("片段 %s 输出残留模板语法字面量（Jet 在中间某行失败）", name)
	}
	return out
}

// TestAttrGroupFormFragmentRendersFailEcho 属性组片段：host + htmx 三分属性 + 错误槽位置 + 回填。
func TestAttrGroupFormFragmentRendersFailEcho(t *testing.T) {
	out := renderAttrFragment(t, "product_attribute_group_form.html", attrGroupFragmentData(true))

	// ① htmx 分档三分属性 + host 容器（host 在 hx-target 里也出现一次，故 ≥2）。
	for _, want := range []string{
		`hx-post="/admin/product-attributes/create"`,
		`hx-target="closest [data-attr-group-host]"`,
		`hx-swap="outerHTML"`,
		`<div data-attr-group-host>`,
		`action="/admin/product-attributes/create"`, // 原生回退：无 JS 时照旧提交
		`name="inDrawer" value="1"`,                 // 抽屉形态申报（失败重渲染后「取消」按钮还在）
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败片段应渲染 %s，实际未见", want)
		}
	}
	if got := strings.Count(out, "data-attr-group-host"); got < 2 {
		t.Errorf("data-attr-group-host 出现 %d 次，want ≥2（容器 + hx-target 选择器）", got)
	}

	// ② 错误槽：在 host 内、表单**之外**（锚在本片段自己的标记上，而不是页面里第一个 <form>）。
	for _, want := range []string{`role="alert"`, "属性组标识已被占用", "data-attr-group-err"} {
		if !strings.Contains(out, want) {
			t.Errorf("失败片段应带错误槽标记 %s", want)
		}
	}
	hostAt := strings.Index(out, "<div data-attr-group-host>")
	errAt := strings.Index(out, "data-attr-group-err")
	formAt := strings.Index(out, "data-attr-group-form")
	if hostAt < 0 || errAt < 0 || formAt < 0 {
		t.Fatalf("片段标记缺失：host=%d err=%d form=%d", hostAt, errAt, formAt)
	}
	if !(hostAt < errAt && errAt < formAt) {
		t.Error("错误槽必须在 host 内、表单之前 —— 放进表单里会被下一次提交整块换掉")
	}

	// ③ 单值回填 + 勾选态按值命中。
	for _, want := range []string{
		`name="name" value="颜色"`,
		`name="key" value="color"`,
		`name="sort" value="3"`,
		`name="projectId" value="pr1"`,
		`name="isVariation" value="1" checked`, // 提交里存在 1 → 勾上
		`name="inDrawer" value="1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("回填应渲染 %s，实际未见", want)
		}
	}
	// 值行也留住（走 RowsCtx 的重建，不是单值回填）。
	if !strings.Contains(out, `name="values[0].label" value="红"`) {
		t.Error("新建抽屉失败时用户刚编的属性值行也必须留住")
	}
	// 取消按钮在（抽屉形态由隐藏域申报 → handler 归还 InDrawer 键）。
	if !strings.Contains(out, "data-drawer-close") {
		t.Error("失败重渲染后「取消」按钮必须还在（否则抽屉关不掉）")
	}
}

// TestAttrGroupFormFragmentUncheckedVariant 未勾选的那一版：按值命中（不能用「字段提交过」判）。
func TestAttrGroupFormFragmentUncheckedVariant(t *testing.T) {
	data := attrGroupFragmentData(true)
	// 这次提交只带同名隐藏域（用户**没**勾「参与变体」）。
	data["FormEchoMulti"] = map[string]any{"isVariation": []string{"0"}}
	data["FormEcho"].(map[string]any)["isVariation"] = "0"
	out := renderAttrFragment(t, "product_attribute_group_form.html", data)

	if strings.Contains(out, `name="isVariation" value="1" checked`) {
		t.Error("用户没勾「参与变体」，回填却勾上了 —— 按「字段提交过」判会被同名隐藏域骗到")
	}
}

// TestAttrGroupFormFragmentFirstPaint 首屏：无错误槽、初值取 Name/Key/Sort/VariationChecked。
func TestAttrGroupFormFragmentFirstPaint(t *testing.T) {
	data := attrGroupFragmentData(false)
	// 编辑形态的首屏：初值就是库里那一行的值。
	data["Mode"] = "edit"
	data["Action"] = "/admin/product-attributes/update"
	data["IsCreate"] = false
	data["GroupID"] = "a1"
	data["Name"] = "尺寸"
	data["Key"] = "size"
	data["Sort"] = 5
	data["VariationChecked"] = false

	out := renderAttrFragment(t, "product_attribute_group_form.html", data)

	if strings.Contains(out, "data-attr-group-err") || strings.Contains(out, `role="alert"`) {
		t.Error("首屏不该出现提交失败的错误槽（SubmitErr 只在失败重渲染时注入）")
	}
	for _, want := range []string{
		`name="name" value="尺寸"`,
		`name="key" value="size"`,
		`name="sort" value="5"`,
		`name="id" value="a1"`,
		`hx-post="/admin/product-attributes/update"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("编辑形态首屏应渲染 %s，实际未见", want)
		}
	}
	if strings.Contains(out, `name="isVariation" value="1" checked`) {
		t.Error("该组本来不参与变体，首屏不该勾上")
	}
	// 编辑形态不带值行编辑器（值走「属性值」抽屉）。
	if strings.Contains(out, `name="values[0].label"`) {
		t.Error("编辑属性组的抽屉不该渲染属性值行编辑器")
	}
	// htmx 分档能力来自表单属性，不是失败时才补。
	if !strings.Contains(out, `hx-target="closest [data-attr-group-host]"`) {
		t.Error("首屏表单也必须有 hx-target（否则第一次提交就退化成整页跳转）")
	}

	// 新建形态的占位文案与编辑形态**不同**（新建给示例「如 颜色」，编辑给中性字段名）：
	// 合并成一份会在某条入口上降级 —— 这条钉住两态各自的原样文案。
	createOut := renderAttrFragment(t, "product_attribute_group_form.html", attrGroupFragmentData(false))
	for _, want := range []string{"属性组名称（如 颜色）", "标识（留空自动生成，如 color）"} {
		if !strings.Contains(createOut, want) {
			t.Errorf("新建抽屉的占位文案应含 %q", want)
		}
	}
	if strings.Contains(out, "属性组名称（如 颜色）") {
		t.Error("编辑抽屉不该用「如 颜色」这种新建示例占位")
	}
}

// TestAttrValuesFormFragmentRendersFailEcho 属性值片段：host + htmx 三分属性 + 行回填。
func TestAttrValuesFormFragmentRendersFailEcho(t *testing.T) {
	out := renderAttrFragment(t, "product_attribute_values_form.html", attrValuesFragmentData(true))

	for _, want := range []string{
		`hx-post="/admin/product-attributes/set-values"`,
		`hx-target="closest [data-attr-values-host]"`,
		`hx-swap="outerHTML"`,
		`<div data-attr-values-host>`,
		`action="/admin/product-attributes/set-values"`,
		`id="attr-values-a1"`, // 行编辑器容器：添加 / 删除行的 hx-include 目标
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败片段应渲染 %s，实际未见", want)
		}
	}
	if got := strings.Count(out, "data-attr-values-host"); got < 2 {
		t.Errorf("data-attr-values-host 出现 %d 次，want ≥2", got)
	}

	hostAt := strings.Index(out, "<div data-attr-values-host>")
	errAt := strings.Index(out, "data-attr-values-err")
	formAt := strings.Index(out, "data-attr-values-form")
	if hostAt < 0 || errAt < 0 || formAt < 0 {
		t.Fatalf("片段标记缺失：host=%d err=%d form=%d", hostAt, errAt, formAt)
	}
	if !(hostAt < errAt && errAt < formAt) {
		t.Error("错误槽必须在 host 内、表单之前")
	}

	// 行回填：两行都还在，且**启用态逐行还原**（禁用也是有效状态，不能被吞掉）。
	for _, want := range []string{
		`name="values[0].label" value="红"`,
		`name="values[1].label" value="蓝"`,
		`name="values[0].enabled" value="1" checked`,
		`name="id" value="a1"`,
		`name="groupId" value="a1"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("失败片段应渲染 %s，实际未见", want)
		}
	}
	if strings.Contains(out, `name="values[1].enabled" value="1" checked`) {
		t.Error("第 2 行被用户明确取消了「启用」，回填不该勾上")
	}
	if !strings.Contains(out, "data-drawer-close") {
		t.Error("失败重渲染后「取消」按钮必须还在")
	}
}

// TestAttrValuesFormFragmentFirstPaint 首屏：无错误槽、初值取库里的值。
func TestAttrValuesFormFragmentFirstPaint(t *testing.T) {
	out := renderAttrFragment(t, "product_attribute_values_form.html", attrValuesFragmentData(false))

	if strings.Contains(out, "data-attr-values-err") || strings.Contains(out, `role="alert"`) {
		t.Error("首屏不该出现提交失败的错误槽")
	}
	if !strings.Contains(out, `name="values[0].label" value="红"`) {
		t.Error("首屏应渲染库里已有的属性值")
	}
	if !strings.Contains(out, `hx-target="closest [data-attr-values-host]"`) {
		t.Error("首屏表单也必须有 hx-target")
	}
}
