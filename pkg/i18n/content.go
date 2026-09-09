package i18n

// content.go — 内容翻译基础设施（多语言 P5a，docs/06-D-site-i18n.md §7）。
//
// 与 sys_i18n 的分工（决策 F17）：
//   - sys_i18n（本包既有能力）= 开发者 key，跟代码发布走，寻址 item_key + lang；
//   - sys_translation（本文件）= 编辑器里写下的用户文本（构建器内联文本 + CMS 字段），
//     跟内容编辑走，寻址 (source_hash, context, lang)。
// 两张表、两套生命周期，不合并；本文件只新增只读取词路径，
// 不触碰 cache.go / loader.go / snapshot.go 的缓存与加载核心。
//
// 兜底铁律（决策 F8，用户最高优先级要求）：
//   - 任何路径都不报错、不 panic、不返回空串；
//   - 有译文用译文，无译文回退 sourceText（原文）；
//   - 表缺失 / 查询失败 / 未初始化 / 空语境的 store → 空索引 → 全部回退原文；
//   - 无 draft/confirmed 状态机：写入行即生效。
//
// 取值链路（§7.6 + §7.7）：
//
//	跳过规则（纯数字/纯符号/空白）→ 语境为空 → 索引命中 → 回退原文
//
// 注意：本文件不负责「收集候选字段」（组件 Translatable 白名单、CMS 字段清单属
// P5b/P5d），只负责「给定原文 + 语境 + 语言，怎么取译文」。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"sync/atomic"
)

// contentSkipValue 跳过规则正则（docs/06-D §7.6）：整体只由数字、标点、符号、空白组成。
//
//	纯数字   2024 / 42 / 3.14 / -5 / 1,024 → 跳过
//	纯符号   → / -- / · / | / …            → 跳过
//	含数字的句子 共 42 件 / 2010 年成立    → 照常翻译
var contentSkipValue = regexp.MustCompile(`^[\p{N}\p{P}\p{S}\s]*$`)

// ContentHash 返回内容寻址指纹：sha256hex(strings.TrimSpace(sourceText))。
//
// 语义（§7.4）：只对原文做 hash，不含 context、不含 lang；写入前统一 TrimSpace，
// 避免「末尾多个空格」被当成不同文本。改原文 → hash 变 → 旧译文不再命中（自动失效）。
// 空串同样返回稳定的 64 位十六进制串（sha256 空输入），调用方是否取词由跳过规则决定。
func ContentHash(sourceText string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceText)))
	return hex.EncodeToString(sum[:])
}

// ShouldTranslateContent 判断字段值是否参与翻译（§7.6 跳过规则）。
//
// 返回 false 的情形：去空白后为空串、整体是纯数字/纯符号/纯空白。
// 这类值不进翻译表，也不计入完成度统计的分母；返回 true 的值才是候选。
func ShouldTranslateContent(sourceText string) bool {
	s := strings.TrimSpace(sourceText)
	return s != "" && !contentSkipValue.MatchString(s)
}

// ContentContext 拼装语境（§7.5）：{类型}.{字段名}，如 core.button.text、product.name。
//
// 任一参数为空（去空白后）返回空串——空语境不参与取词（取词器直接回退原文），
// 避免「无语境」把页脚按钮的译法误用到商品 CTA（§7.7「不跨语境回退」）。
// 嵌套字段按 D11「不带索引」：core.buttonList.text。
func ContentContext(typ, field string) string {
	typ = strings.TrimSpace(typ)
	field = strings.TrimSpace(field)
	if typ == "" || field == "" {
		return ""
	}
	return typ + "." + field
}

// contentIndexSep 索引键分隔符（NUL）：docs/06-D §7.7 的 source_hash + "\x00" + context。
// hash 是十六进制、context 是「类型.字段名」，都不会含 NUL，故该键无歧义。
const contentIndexSep = "\x00"

// ContentIndexKey 返回内存索引键：source_hash + NUL + context（§7.7 示意）。
//
// 同一 hash 的不同 context 各自成键，不互相覆盖（不跨语境回退的前提）。
func ContentIndexKey(sourceHash, contextName string) string {
	return sourceHash + contentIndexSep + contextName
}

// ContentTranslator 一次构建（一种语言）的内容译文取词器：批量预载 + 内存索引 + 回退原文。
//
// 生命周期：构建开始时用候选 hash 集合构造一次（一次 SQL），构建期间只读内存；
// 组件渲染期不再查库（§7.7「零查库」）。构建中途改表不影响已构造的取词器（防线 4）。
type ContentTranslator struct {
	lang   string
	index  map[string]string
	misses atomic.Int64
}

// NewContentTranslator 用默认存储（sys_translation 表）批量预载译文。
//
// hashes 为本轮构建所有候选原文的 ContentHash 集合；一次查询取回全部命中，
// 查询失败 / 表缺失 / 数据库未初始化 → 空索引（全部回退原文），不返回 error。
func NewContentTranslator(ctx context.Context, lang string, hashes []string) *ContentTranslator {
	return NewContentTranslatorWith(ctx, defaultContentStore(), lang, hashes)
}

// NewContentTranslatorWith 用注入的存储预载（测试或自定义来源）。
//
// store 为 nil / 返回错误 → 空索引（全部回退原文），绝不 panic、绝不报错。
func NewContentTranslatorWith(ctx context.Context, store ContentStore, lang string, hashes []string) *ContentTranslator {
	t := &ContentTranslator{lang: strings.TrimSpace(lang), index: map[string]string{}}
	if store == nil {
		return t
	}

	targets, err := store.LoadTargets(ctx, t.lang, hashes)
	if err != nil {
		// 兜底：内容翻译不可用不能拖垮构建（原文照常输出）。
		logContentStoreFailure(t.lang, err)
		return t
	}

	for key, target := range targets {
		if target != "" {
			t.index[key] = target
		}
	}
	return t
}

// TranslateContent 构建期取词入口（§7.7 第 5 步）：有译文用译文，无译文回退原文。
//
// 链路：
//  1. t == nil（调用方没预载 / 装配缺失）→ 返回 sourceText；
//  2. 跳过规则命中（纯数字/纯符号/空白）→ 返回 sourceText（这类值本就不该进表）；
//  3. contentContext 为空 → 返回 sourceText（不跨语境回退）；
//  4. 索引命中 (hash, context) 且译文非空 → 返回译文；
//  5. 其余（未命中 / 译文为空）→ 返回 sourceText，缺失计数 +1（L3 诊断用）。
//
// 不返回空串：sourceText 为空时原样返回空串（无内容可回退），
// 只要原文非空，本方法任何路径都返回非空串。
func (t *ContentTranslator) TranslateContent(sourceText, contentContext string) string {
	if t == nil {
		return sourceText
	}
	if !ShouldTranslateContent(sourceText) {
		return sourceText
	}

	contentContext = strings.TrimSpace(contentContext)
	if contentContext == "" {
		return sourceText
	}

	if target, ok := t.index[ContentIndexKey(ContentHash(sourceText), contentContext)]; ok && target != "" {
		return target
	}

	t.misses.Add(1)
	return sourceText
}

// Lang 返回本取词器绑定的目标语言（空=调用方未指定）。
func (t *ContentTranslator) Lang() string {
	if t == nil {
		return ""
	}
	return t.lang
}

// Size 返回预载命中的译文条数（索引大小，L3 诊断用）。
func (t *ContentTranslator) Size() int {
	if t == nil {
		return 0
	}
	return len(t.index)
}

// Misses 返回构建期取词未命中次数（跳过规则命中的取值不计入，§7.6）。
//
// 供 P5b 的 L3「构建期扫描 + 缺失统计告警」消费；并发安全（原子计数）。
func (t *ContentTranslator) Misses() int64 {
	if t == nil {
		return 0
	}
	return t.misses.Load()
}
