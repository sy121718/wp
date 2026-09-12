// 由 go run ./cmd/workbench-contracts 生成，请修改组件 Go 声明。
// 此文件随源码提交；检查：go run ./cmd/workbench-contracts -check
export const alignedRepeaters = {
    "core.accordion": {
        "type": "core.accordion",
        "alignKey": "items",
        "field": "title",
        "noun": "折叠项",
        "label": "标题",
        "addText": "+ 添加折叠项（自动创建内容）",
        "extra": [
            {
                "key": "open",
                "label": "默认展开"
            }
        ]
    },
    "core.tabs": {
        "type": "core.tabs",
        "alignKey": "tabs",
        "field": "label",
        "noun": "页签",
        "label": "标签",
        "addText": "+ 添加页签（自动创建面板）"
    }
};
