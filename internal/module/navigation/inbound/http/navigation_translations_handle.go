package navigationhttp

// navigation_translations_handle.go — 导航写请求的**形状基座**。
//
// 译文保存已经下沉到 navigation service（TranslationWriter.WriteTranslations），
// 成功回执也改成「现成结果直发」（key + contract 事实拷贝，见 navigation_handle.go），
// 于是 HTTP 层不再需要 Go 侧拼句工具 —— navigationPlaceholderText / navigationFirstPlaceholder
// 已随之删除。本文件只剩三条写路径（Create / Delete / move）共用的顶层键读取：
// navigationWriteBody / navigationWriteAction / navigationWriteRequiredString ——
// 它们读的是**形状**，不判业务，也不碰数据库。
//
// 判别本身已迁到 dto（UpdateEnvelope.Route()，严格 tagged union：缺 action = legacy、
// action=translations / move，其余一律拒绝；legacy 档对未知键仍宽容，译文档顶层严格），
// 这里不再持有第二份判别依据。
//
// 旧形态（包级可变写句柄 + 它的包级注入点 + 页面句柄的三段自动提交）已删除：
// 写路径现在走一条事务，写服务由装配层经 Handle.SetTranslationWriteService 注入。
//
// 这里也不再提那两个已删符号的名字 —— 源码级判据（TestNavigationUpdateTranslationsHaveNoLegacyAdapter）
// 正是按字面串扫这两个文件的：在注释里留一句「旧的 xxx 已删除」会让它永远红，
// 而它要守的事实（只剩一条写路径）其实是满足的。清理旧形态时连注释一起清。

import (
	"bytes"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	json "go_wp/pkg/jsonx"
	"go_wp/pkg/logger"
	"go_wp/pkg/response"
)

const (
	// navigationWriteMaxBodyBytes 请求体上限（1 MiB）。
	//
	// 行数上限（1000）在 dto 的行形状解析里判，而且判在解析**之后**；这里挡的是解析
	// **之前**的字节量（否则一份很大的 body 会先进内存，再被行数判据拒绝）。
	navigationWriteMaxBodyBytes = 1 << 20
)

// navigationWriteBody 读一次请求体并解析成顶层键值。
//
// 空体 / 超过 1 MiB / 非法 JSON / 不是对象（数组、字符串、null）→ 400 并返回 false。
func navigationWriteBody(c *gin.Context) ([]byte, map[string]json.RawMessage, bool) {
	if c == nil || c.Request == nil || c.Request.Body == nil {
		return nil, nil, false
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, navigationWriteMaxBodyBytes+1))
	if err != nil {
		logger.Scene("navigation").Error(err, "读取导航写请求体失败")
		response.ParamError(c)
		return nil, nil, false
	}
	if len(body) > navigationWriteMaxBodyBytes {
		// 只记字节数：body 里可能有运营填的整表译文，原文不入日志。
		logger.Scene("navigation").With("bytes", len(body)).Warn("导航写请求体超过上限，已拒绝")
		response.ParamError(c)
		return nil, nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil || raw == nil {
		response.ParamError(c)
		return nil, nil, false
	}
	return body, raw, true
}

// navigationWriteAction 读 action 键。返回 (动作, 是否出现该键, 形状是否合法)。
//
// 形状判据刻意严格：null / 空串 / 数字 / 布尔 / 对象 / 数组一律非法 —— 「判别键写得模棱两可」
// 时唯一安全的动作是拒绝，而不是替调用方猜他想走哪个分支。
func navigationWriteAction(raw map[string]json.RawMessage) (string, bool, bool) {
	value, exists := raw["action"]
	if !exists {
		return "", false, true
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", true, false
	}
	var action string
	if err := json.Unmarshal(value, &action); err != nil {
		return "", true, false
	}
	action = strings.TrimSpace(action)
	if action == "" {
		return "", true, false
	}
	return action, true, true
}

// navigationWriteRequiredString 读一个必需的字符串字段（缺键 / null / 非字符串 / 空 → 拒）。
func navigationWriteRequiredString(raw map[string]json.RawMessage, key string) (string, bool) {
	value, ok := raw[key]
	if !ok {
		return "", false
	}
	if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}
