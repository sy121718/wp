// Package backpath 写操作之后的**回跳地址**构造：站内路径 + 按调用点白名单透传的查询参数。
//
// 为什么值得抽出来：回跳地址过去被当成「客户端给的值」——页面渲染时先用 Go 算一个
// returnQuery 串塞进隐藏域，POST 回来再解析它、按**每个模块各自一份**的白名单 map 过滤键名
// 再拼出目标 URL（order 的 couponBackQuery / couponEchoQuery / couponBackKeys 是样板，
// 三个模块各抄一遍）。服务端本来就知道自己在处理哪个工程、哪一页筛选 —— 上下文随表单
// action 的 query 一起提交，按调用点**显式列出的键**读回来即可：白名单从「每个模块一份
// map」变成「调用点那一行参数」，而形状收敛（去空白、长度上限、控制字符）只实现一次。
//
// 本包是**纯构造**：不 import gin、不读 *gin.Context、不记日志。
//   - 取查询（`c.Request.URL.Query()`）、非法路径的 Warn 与控制面首页 `/admin` 兜底留在
//     壳层 / 调用方 —— `/admin` 是控制面概念，不该由 pkg 替调用方选落点；
//   - 路径判据（站内相对路径、长度上限）复用 pkg/safepath，与语言切换回跳、提示页跳转
//     共用同一份，不在这里复写；
//   - 「哪个值被丢弃」是可观测性：判据以 KeepValue 导出，调用方记日志时用的是同一份。
package backpath

import (
	"net/url"
	"strings"

	"go_wp/pkg/safepath"
)

// MaxQueryValueBytes 单个查询值允许的最大字节数。
//
// 200 字节：后台筛选值（工程 id / 状态枚举 / 关键词 / 页码）都远小于它；这个上限挡住的是
// 「让管理员点一个带几 KB query 的链接，回跳时再把它拼回去」。
const MaxQueryValueBytes = 200

// Build 拼回跳地址：path + 从 source 里按 keys 透传的参数。
//
// 返回 (回跳地址, true)，或 ("", false) 表示 **path 本身不合法**（不是站内相对路径、
// 超过 safepath.MaxReturnPathBytes、含控制字符）。调用点写的是自己代码里的字面量，
// 非法属于编程错误 —— 但不能因此把用户打成 500：**日志与兜底落点由调用方决定**，
// 本包不替它选（过去这里回控制面首页，那是壳层的知识）。
//
// 值级规则（与迁移前逐条一致）：
//   - trim 后为空 → 丢弃，且**静默**（避免产出 `?status=&page=` 这种噪音，也不该刷日志）；
//   - 超过 MaxQueryValueBytes 或含控制字符（C0/DEL）→ 丢弃（调用方按 KeepValue 记 Warn）；
//   - 其余按 trim 后的值透传，**值一律不做语义解释**（是不是合法状态码由页面按老规矩校验）。
//
// 两个既有行为按现状保留（迁移不改语义）：
//   - 同一键在 source 里重复出现时取**第一个**（url.Values.Get 语义）；
//   - path 里自带 query 时不解析、直接当字符串拼（见 WithParams 注释）。
//
// source 为 nil 安全（url.Values(nil).Get 返回空串）。
func Build(path string, source url.Values, keys ...string) (string, bool) {
	if !safepath.IsSameOriginPath(path) || len(path) > safepath.MaxReturnPathBytes {
		return "", false
	}
	params := make(map[string]string, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(source.Get(k))
		if v == "" {
			continue
		}
		if !KeepValue(v) {
			continue
		}
		params[k] = v
	}
	return WithParams(path, params), true
}

// KeepValue 值级判据：trim 过的值是否可以透传（非空、≤ MaxQueryValueBytes、无控制字符）。
//
// Build 内部用的就是它；调用方记「哪个值被丢弃」的日志时也用**它**，这样「构造时丢掉的
// 值」与「日志里说丢掉的值」永远是同一批 —— 判据只有一份，谁都不会各自漂移。
func KeepValue(v string) bool {
	if v == "" {
		return false
	}
	return len(v) <= MaxQueryValueBytes && !safepath.HasControlRune(v)
}

// WithParams 给站内路径追加查询参数：空值丢弃，编码走 url.Values（键有序，产物稳定）。
//
// 单独导出是因为调用点常有一个「不来自 query 的值」要带回去（例如从 PostForm 读到的
// projectId），拼法必须与 Build 一致 —— 两处各拼一份就会出现「有的参数被转义、有的没有」
// 这种只在特定值上才暴露的差异。
//
// path 里**不要**自带 query：本函数只做 `path + "?" + Encode()`，传 "/a?x=1" 会拼出
// "/a?x=1?k=v"。这是既有行为，M2 按现状保留（调用点传的都是裸路径）。
func WithParams(path string, params map[string]string) string {
	if len(params) == 0 {
		return path
	}
	q := url.Values{}
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		q.Set(k, v)
	}
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
