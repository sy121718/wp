package pageservice

// page_lang.go — 站点产物语言装配（多语言 P2，docs/06-D）。
//
// 语言是构建环境维度：构建、预览、发布都必须显式携带目标语言，产物路径前缀
// 单点经 pipeline.LangPath 计算（禁止各处手拼 "/" + lang + path）。
// 配置开关 i18n.site_lang_prefix（pkg/i18n.SiteLangPrefixEnabled）是决策 D1 的
// 落地闸门：关闭时保持逻辑路径（单语言兼容），开启后全语言带 /{lang}/ 前缀。

import (
	"context"
	"strings"

	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
)

// buildLang 解析本次构建语言：请求显式指定优先，否则站点默认语言（i18n.default_lang）。
func buildLang(requested string) string {
	lang := strings.TrimSpace(requested)
	if lang == "" {
		return i18n.GetDefaultLang()
	}
	return lang
}

// sitePath 逻辑访问路径 → 实际访问路径（语言前缀开关见文件头）。
// 关闭：返回规范化逻辑路径；开启：/{lang}/path（语言根映射 /{lang}/index）。
func sitePath(lang, logical string) (string, error) {
	if !i18n.SiteLangPrefixEnabled() {
		return pipeline.NormalizeURL(logical)
	}
	return pipeline.LangPath(lang, logical)
}

// siteRoutePath 保存草稿/建页阶段的路径占用路径：此时语言未知，用站点默认语言。
// 多语言站点每语言一行占用属后续阶段（docs/06-D §14 D10 语言清单）；
// 当前只登记默认语言那一行，发布时按构建语言登记激活行。
func siteRoutePath(logical string) (string, error) {
	return sitePath(i18n.GetDefaultLang(), logical)
}

// highlightPath 导航「当前项」高亮用的访问路径：与导航项 URL 同源（带前缀）。
// 路径非法或空时返回空串（不标记当前项，绝不让高亮逻辑影响构建主链）。
func highlightPath(lang, logical string) string {
	if strings.TrimSpace(logical) == "" {
		return ""
	}
	p, err := sitePath(lang, logical)
	if err != nil {
		return ""
	}
	return p
}

// localizeMenuURL 导航项 URL 本地化：站内绝对路径（以 / 开头）加语言前缀，
// 外链（http/https///mailto/tel/#）与相对路径原样保留。
//
// 必要性：导航 URL 来自 navigation 表，存的是站点逻辑路径；多语言开启前缀后
// 产物里的菜单链接必须指向 /{lang}/path，否则访客点菜单必然 404。
func localizeMenuURL(lang, raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return raw
	}
	p, err := sitePath(lang, u)
	if err != nil {
		return raw
	}
	return p
}

// MarkStaleForI18n 把全部页面标记为待重建（文案词条变更后调用）。
//
// 与 Manifest 的 i18n 依赖条目（DependencyKind=i18n）配套：
// 依赖条目负责「产物字节与词条 revision 的对应关系」，本方法负责「变更后重新排队」。
// 调用方：后台 i18n CRUD（docs/06-D §14 D7，尚未实现）或运维脚本。
func (s *Service) MarkStaleForI18n(ctx context.Context) error {
	return s.model.MarkStaleForI18n(ctx)
}

// buildDependencies 构建期依赖（pipeline.DependencyProvider 实现）。
// 组件固定文案由构建期取词注入 HTML 字节，因此产物依赖文案词条资源版本号：
// 改文案 → revision 变化 → 依赖比对不等 → 触发重建（docs/06-D §10.4）。
func (s *Service) buildDependencies(_ context.Context, _ pipeline.BuildInput) []pipeline.Dependency {
	return []pipeline.Dependency{pipeline.I18NDependency(i18n.Revision())}
}
