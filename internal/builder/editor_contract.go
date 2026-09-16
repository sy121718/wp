// 工作台「对齐重复项」契约生成（tabs / accordion 的 AlignedRepeaterSpec）。
//
// 本文件只覆盖对齐重复项，不是编辑器的完整组件契约。完整组件 schema 见
// ComponentSchemas 与 cmd/workbench-contracts 的另一条生成线；文件名 editor_contract.go
// 是历史遗留，阅读时勿与 ComponentSchemas 混淆。
package builder

import (
	"encoding/json"
	"fmt"

	"go_wp/internal/builder/core"
)

// EditorContractsJS 从已注册组件生成工作台元数据，无数据库或 Node 依赖。
// encoding/json 按 map 键排序，生成结果不含时间戳，便于逐字节检查漂移。
func EditorContractsJS() ([]byte, error) {
	specs := map[string]*core.AlignedRepeaterSpec{}
	for _, name := range core.Types() {
		if spec := core.AlignedRepeaterFor(name); spec != nil {
			specs[name] = spec
		}
	}
	data, err := json.MarshalIndent(specs, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("生成工作台契约: %w", err)
	}
	palette, err := PaletteSpecJS()
	if err != nil {
		return nil, err
	}
	return []byte("// 由 go run ./cmd/workbench-contracts 生成，请修改组件 Go 声明。\n" +
		"// 此文件随源码提交；检查：go run ./cmd/workbench-contracts -check\n" +
		"export const alignedRepeaters = " + string(data) + ";\n\n" + string(palette)), nil
}

// PaletteSpecJS 组件库元数据的 JS 片段（审计 REG-005）。
//
// 组件库条目里的显示名 / 说明 / 分组 / 默认 Props 全部来自组件声明
// （core.AtomSpec 或组件的 Palette() 方法），前端不再手写组件条目：
// 新增组件只改 Go，重新生成本文件后即出现在组件库。
// 前端 palette.js 只保留自动化不了的人工信息（分组顺序、结构型默认子树）。
func PaletteSpecJS() ([]byte, error) {
	items, err := ComponentPalette()
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(map[string]any{"items": items}, "", "    ")
	if err != nil {
		return nil, fmt.Errorf("生成组件库元数据: %w", err)
	}
	return []byte("export const paletteSpec = " + string(data) + ";\n"), nil
}
