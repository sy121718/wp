// Package sysconfigdto 定义 sysconfig 模块的请求 / 响应结构（数据流 inbound → service → inbound）。
//
// 本模块的形状只有一种：**配置分组**（group_key + 整组 JSON + 乐观锁版本号）。
// 组内键（default_lang / site_lang_url_mode …）是**业务语义键**，各消费方自己解释；
// 本层不解释它们，也不做白名单校验 —— 白名单属于消费方（读的人知道哪些键有意义，
// 认不出的键一律忽略），写进来一堆没人认的键不会让谁出错。
package sysconfigdto

import "go_wp/pkg/utils"

// Group 一个配置分组（后台一次保存的单元）。
type Group struct {
	// Key 分组键（唯一）。
	Key string `json:"key"`
	// Name 分组显示名（后台页面展示）。
	Name string `json:"name"`
	// Data 整组配置的 JSON 对象。
	Data map[string]any `json:"data"`
	// Remark 备注。
	Remark string `json:"remark"`
	// Status 状态：1 启用 / 0 禁用。
	Status int `json:"status"`
	// Version 乐观锁版本号：保存时必须带上读到的值，库内已变则拒绝。
	Version int64 `json:"version"`
	// UpdateTime 最后修改时间（对外只到秒）。
	UpdateTime utils.JSONTime `json:"updateTime"`
}

// SetGroupReq 整组保存请求。
//
// Version 是**必填**：不带版本的整组保存等于「用我手里这份覆盖别人的改动」，
// 而这正是乐观锁要拦的事 —— 缺失即拒绝，不给「省略即强制覆盖」留口子。
type SetGroupReq struct {
	// GroupKey 分组键。
	GroupKey string `json:"groupKey"`
	// Data 新的整组配置（整体替换，不是合并）。
	Data map[string]any `json:"data"`
	// Version 调用方读到的版本号（乐观锁前置条件）。
	Version int64 `json:"version"`
	// UpdateBy 操作人 ID（0 = 系统 / 未登录）。
	UpdateBy int64 `json:"updateBy"`
}
