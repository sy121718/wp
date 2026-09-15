package contentservice

// content_translate.go — 内容实体的字段级批量取词（审计 I18N-006）。
//
// 完全对称商品域的做法（product/service/entity_source_translate.go）：
//   · 可翻译字段在 contract 里**单处声明**（IsTranslatableField），不在这里再列一份；
//   · 构建期按 lang **一次预载**译文再回填 —— 逐个字段查会按字段数放大成 N 次 SQL，
//     而内容详情页构建时每个绑定字段都会走这条路；
//   · 改译文触发 stale：sys_translation 的 update_time 推进 ContentRevision，
//     构建期的依赖比对随之失效（依赖追踪与商品域同一套，无需另接）。
//
// 只处理**字符串字段**：contents.data 是 JSONB，值可能是数组 / 对象 / 数字。
// 拿非字符串去查译文表不只是白费功夫 —— 把结构值当原文哈希写进索引才是真的错。

import (
	"context"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	"go_wp/pkg/i18n"
)

// SetContentStore 注入内容译文存储（装配期；可空）。
//
// 为空时不做翻译、逐字节回退原文 —— 与接入前完全一致（未接译文不应改变产物）。
func (s *Service) SetContentStore(store i18n.ContentStore) { s.contentStore = store }

// translateData 返回字段取过译文的副本（原 map 不动：同一份 data 可能被多处引用）。
func (s *Service) translateData(ctx context.Context, lang, entityType string, data map[string]any) map[string]any {
	if len(data) == 0 {
		return data
	}
	fields := contentcontract.TranslatableFields(entityType)
	if len(fields) == 0 {
		return data
	}
	type job struct {
		field       string
		source      string
		contextName string
	}
	jobs := make([]job, 0, len(fields))
	hashes := make([]string, 0, len(fields))
	for _, f := range fields {
		v, ok := data[f]
		if !ok {
			continue
		}
		src, ok := v.(string)
		if !ok || !i18n.ShouldTranslateContent(src) {
			continue // 非字符串 / 空 / 纯符号：本就不该进译文表
		}
		jobs = append(jobs, job{field: f, source: src, contextName: i18n.ContentContext(entityType, f)})
		hashes = append(hashes, i18n.ContentHash(src))
	}
	if len(jobs) == 0 {
		return data
	}
	// 工程作用域（审计 I18N-009）：工程 id 从构建上下文取（core.WithBuildProjectID
	// 由 builder.Compile 注入，与 BuildLang 同一约定，签名里不再多一个易失配的参数）。
	// 非构建调用（后台预览等）拿不到工程 id 时退化为全局视图，与接入前一致。
	tr := i18n.NewContentTranslatorScoped(ctx, core.BuildProjectID(ctx), s.contentStore, lang, hashes)
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	for _, j := range jobs {
		target := tr.TranslateContent(j.source, j.contextName)
		// 富文本译文的清洗（审计 I18N-006）：译文来自翻译工作台，与正文一样是**不可信输入**。
		// 原文过清洗不代表译文也干净 —— 工作台是另一个入口，AI / PO 导入的译文更要过这道关。
		// 只对有译文的字段做（回退原文时原文已经洗过一遍，重复清洗等于白跑一次解析器）。
		if target != j.source && contentcontract.IsRichTextField(entityType, j.field) {
			target = core.SanitizeRichHTML(target)
		}
		out[j.field] = target
	}
	return out
}
