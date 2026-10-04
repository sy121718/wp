package mcp

// validate_test.go — 参数校验（安全边界）的用例。
//
// 这些用例钉的是「模型不能通过参数表达什么」，不是「校验函数好不好用」：
// 多传一个字段、把整数写成小数、把枚举值写成别的词，都必须被拒绝。

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// orderArgsSchema 一个既有必填、又有可选与枚举的真实形状（订单区间摘要）。
func orderArgsSchema() Schema {
	return Object("订单区间摘要参数", map[string]Schema{
		"projectId": String("站点工程 id"),
		"from":      String("起始日 YYYY-MM-DD，含当天"),
		"to":        String("结束日 YYYY-MM-DD，含当天"),
		"limit":     Integer("最多返回多少行"),
		"verbose":   Boolean("是否返回明细"),
		"scope":     Enum("统计口径", "paid", "all"),
		"tags":      Array("标签过滤", String("标签")),
	}, "projectId", "from", "to")
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		schema  Schema
		args    string
		wantErr string // 期望错误里出现的关键词；空 = 应当通过
	}{
		{
			name:   "必填齐全：通过",
			schema: orderArgsSchema(),
			args:   `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30"}`,
		},
		{
			name:   "可选项齐全：通过",
			schema: orderArgsSchema(),
			args:   `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","limit":10,"verbose":true,"scope":"paid","tags":["a","b"]}`,
		},
		{
			name:   "空参数对零参数工具：通过",
			schema: Object("无参数", map[string]Schema{}),
			args:   ``,
		},
		{
			name:    "缺必填：拒绝并点名",
			schema:  orderArgsSchema(),
			args:    `{"from":"2026-09-01","to":"2026-09-30"}`,
			wantErr: `缺少必填参数 "projectId"`,
		},
		{
			name:    "必填传 null：等同没传",
			schema:  orderArgsSchema(),
			args:    `{"projectId":null,"from":"2026-09-01","to":"2026-09-30"}`,
			wantErr: `缺少必填参数 "projectId"`,
		},
		{
			name:    "未知字段：拒绝（不静默忽略）",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","sortExpr":"total desc"}`,
			wantErr: `未知参数 "sortExpr"`,
		},
		{
			name:    "整数字段收到小数：拒绝",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","limit":1.5}`,
			wantErr: "必须是整数",
		},
		{
			name:    "整数字段收到字符串：拒绝",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","limit":"10"}`,
			wantErr: "必须是整数",
		},
		{
			name:    "布尔字段收到字符串：拒绝",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","verbose":"true"}`,
			wantErr: "必须是布尔值",
		},
		{
			name:    "枚举外的取值：拒绝并列出白名单",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","scope":"refunded"}`,
			wantErr: "取值必须是 paid / all 之一",
		},
		{
			name:    "数组元素类型不符：拒绝并给出位置",
			schema:  orderArgsSchema(),
			args:    `{"projectId":"p1","from":"2026-09-01","to":"2026-09-30","tags":["a",3]}`,
			wantErr: "第 2 项",
		},
		{
			name:    "顶层不是对象：拒绝",
			schema:  orderArgsSchema(),
			args:    `["p1"]`,
			wantErr: "必须是 JSON 对象",
		},
		{
			name:    "参数不是合法 JSON：拒绝",
			schema:  orderArgsSchema(),
			args:    `{"projectId":`,
			wantErr: "必须是 JSON 对象",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.schema, json.RawMessage(tt.args))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("不该报错: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("应当报错（期望含 %q）", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("错误应含 %q，实得 %q", tt.wantErr, err.Error())
			}
			// 参数类错误要能被 agent loop 与「系统故障」区分开 —— 否则模型会把
			// 查库失败也当成「我参数写错了」，一遍遍重试同样的调用。
			var argsErr *ArgsError
			if !errors.As(err, &argsErr) {
				t.Fatalf("参数错误应是 *ArgsError，实得 %T", err)
			}
		})
	}
}
