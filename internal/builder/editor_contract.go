package builder

import (
	"fmt"
	json "go_wp/pkg/jsonx"

	"go_wp/internal/builder/core"
)

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
