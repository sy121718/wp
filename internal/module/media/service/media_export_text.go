package mediaservice

// 导出物文案：写进 zip，出口是字节流而非模板。
//
// media 的产品级出口有两类：JSON（走 pkg/response 统一取词）与**打包下载**。
// 后者把 README.txt 直接写进 zip 的字节流里，物理上不经过 Jet 模板，模板的全局
// t / tf 取不到 —— 所以本文件是全域唯一允许出现 i18n.Translate 的地方。
// `rg 'i18n\.Translate' internal/module/media/service/` 在本文件命中是**预期**的，
// 不要为了「取词归零」把它清掉：清掉之后 zip 里就只剩裸 key 或中文硬编码。
//
// 三条纪律：
//  1. 每个 key 都要有 zh-CN / en-US 词条（public/migrations 里登记），缺词时
//     fallback 必须是非空的人话，不能是 key 本身 —— 见 media_export_text_test.go。
//  2. 只在 service 内部调用；controller 一行都不许取词（它只把 plan 写成字节流）。
//  3. 语言由调用方按请求解出后一路传下来（lang 形参），本文件不读 gin.Context。

import (
	"strings"

	mediaenums "go_wp/internal/module/media/enums"
	mediamodel "go_wp/internal/module/media/model"
	"go_wp/pkg/i18n"
)

// variantStatusTexts 是变体状态 → 词条 key + 兜底文案的对照表。
// 兜底写的是**中文成品句**而不是空串：词库还没装载（比如首次启动、或迁移未跑完）
// 时，zip 里至少是一句能读懂的话，而不是 admin.media.variantStatus.pending。
var variantStatusTexts = []struct{ Status, Key, Fallback string }{
	{mediamodel.VariantStatusPending, "admin.media.variantStatus.pending", "排队中（pending）"},
	{mediamodel.VariantStatusProcessing, "admin.media.variantStatus.processing", "生成中（processing）"},
	{mediamodel.VariantStatusFailed, "admin.media.variantStatus.failed", "生成失败（failed）"},
}

func variantStatusText(status, lang string) string {
	for _, s := range variantStatusTexts {
		if s.Status == status {
			return i18n.Translate(s.Key, s.Fallback, lang)
		}
	}
	return i18n.Translate(mediaenums.VariantStatusMissing, "无变体记录（missing）", lang)
}

// buildVariantReadme 生成某一变体目录里的 README.txt（该变体缺失或不可用时）。
func buildVariantReadme(dirName, status, lang string) string {
	tr := func(key, fallback string) string { return i18n.Translate(key, fallback, lang) }
	var b strings.Builder
	b.WriteString(tr(mediaenums.PackageTitle, "go_wp 媒体资源包"))
	b.WriteString("\n====================\n")

	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageDir, "目录：{name}",
		map[string]string{"name": dirName}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageVariantType, "内容：{name} 变体",
		map[string]string{"name": zipDirVariantType(dirName)}))
	b.WriteString("\n")
	b.WriteString(i18n.FillTranslate(tr, mediaenums.PackageStatus, "状态：{name}",
		map[string]string{"name": variantStatusText(status, lang)}))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageVariantUnavailable, "说明：该变体文件当前不可用，原图见 original/ 目录。"))
	b.WriteString("\n")
	b.WriteString(tr(mediaenums.PackageRegenHint, "可在媒体库详情页点「重新生成变体」，生成完成后重新下载。"))
	b.WriteString("\n")
	return b.String()
}

// originalReadmeText 是 original/ 目录里那张说明卡的正文（原图路径非法或文件已丢失）。
func originalReadmeText(kind, lang string) string {
	switch kind {
	case originalReadmePathInvalid:
		return i18n.Translate(mediaenums.PackageOriginalPathInvalid, "原图存储路径非法，无法打包。\n", lang)
	case originalReadmeMissing:
		return i18n.Translate(mediaenums.PackageOriginalMissing, "原图物理文件已缺失，仅剩元数据。\n", lang)
	}
	return ""
}

// PackageReadFailedText 是打包时某个条目读不出来时，写进 zip 顶替该文件的说明。
//
// 它原来是 controller 里的 inline 取词（media_handle.go:277），挪到这里的理由：
// controller 一行都不许取词，而这段文案同样是**写进字节流**的导出物文案，
// 与本文件其余几条同属一类。语言由调用方传 response.RequestLanguage(c)。
func PackageReadFailedText(name, lang string) string {
	text, ok := i18n.FillNamedPlaceholders(
		i18n.Translate(mediaenums.PackageReadFailed, "文件读取失败： {name}\n", lang),
		map[string]string{"name": name})
	if !ok {
		return "文件读取失败： " + name + "\n"
	}
	return text
}

// originalReadmeKind 的取值：original/ 下 README.txt 的两种成因。
const (
	originalReadmePathInvalid = "pathInvalid"
	originalReadmeMissing     = "missing"
)
