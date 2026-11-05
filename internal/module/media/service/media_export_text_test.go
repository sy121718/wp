package mediaservice

import (
	"strings"
	"testing"

	"go_wp/pkg/i18n"
)

// 本文件守 A-2 这条纪律：zip 里写出去的必须是能读懂的**人话**，不是词条 key。
//
// 为什么这条要单独测：导出物文案走的是字节流（zip 里的 README.txt），不经过 Jet
// 模板，模板的全局 t / tf 取不到，所以它只能靠 service 自己取词。一旦哪条 fallback
// 退化成空串（历史上就有过 `i18n.TranslateFunc(...)(key, "")` 那种写法），
// `i18n.Translate` 在词库未装载时会**原样返回 key** —— 用户下载的 zip 里就会出现
// `admin.media.variantStatus.pending` 这种东西，而且没有任何日志。

// assertNoBareKey 断言文案里没有词条 key 的裸串。
//
// 判据用「出现 admin.media. 前缀」而不是逐个比对已知 key：本域的界面词条全部在这个
// 命名空间下，出现任何一种就说明取词链路断了（无论断在哪一条）。
func assertNoBareKey(t *testing.T, what, text string) {
	t.Helper()
	if text == "" {
		t.Fatalf("%s 是空串 —— 空 fallback 会让 zip 里只剩空白", what)
	}
	if strings.Contains(text, "admin.media.") {
		t.Errorf("%s 里出现了裸词条 key：%q", what, text)
	}
}

// TestExportTextsSurviveEmptyGlossary 在**词库为空**的前提下取词，验证每条文案都
// 还有人话兜底。empty glossary 是最坏情形：首次启动、或迁移还没跑完。
func TestExportTextsSurviveEmptyGlossary(t *testing.T) {
	// 清空并还原：InjectForTest 是直接赋值不合并，传 nil 等于把整表置空。
	i18n.InjectForTest(nil, nil)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	for _, lang := range []string{"zh-CN", "en-US"} {
		t.Run("variantStatus/"+lang, func(t *testing.T) {
			for _, status := range []string{
				"pending", "processing", "failed",
				"", "some-unknown-status",
			} {
				assertNoBareKey(t, "变体状态文案", variantStatusText(status, lang))
			}
		})

		t.Run("variantReadme/"+lang, func(t *testing.T) {
			// 四种变体目录各生成一次，覆盖 buildVariantReadme 里每一处 FillTranslate。
			for _, dir := range []string{"thumb", "small", "medium", "full"} {
				assertNoBareKey(t, "变体 README("+dir+")", buildVariantReadme(dir, "pending", lang))
			}
			// 未知目录名也要能生成（zipDirVariantType 返回空串的分支）。
			assertNoBareKey(t, "变体 README(未知目录)", buildVariantReadme("unknown", "", lang))
		})

		t.Run("originalReadme/"+lang, func(t *testing.T) {
			assertNoBareKey(t, "原图路径非法", originalReadmeText(originalReadmePathInvalid, lang))
			assertNoBareKey(t, "原图文件缺失", originalReadmeText(originalReadmeMissing, lang))
			// 未知 kind 返回空串是内部契约（调用方只传两个常量），这里钉住它不被误用。
			if got := originalReadmeText("bogus", lang); got != "" {
				t.Errorf("未知 kind 应返回空串，实际 %q", got)
			}
		})

		t.Run("packageReadFailed/"+lang, func(t *testing.T) {
			text := PackageReadFailedText("thumb/a.webp", lang)
			assertNoBareKey(t, "读取失败占位文案", text)
			// 占位符必须真的被替换掉，且带上条目名 —— 否则用户不知道是哪个文件坏了。
			if strings.Contains(text, "{name}") {
				t.Errorf("占位符未替换：%q", text)
			}
			if !strings.Contains(text, "thumb/a.webp") {
				t.Errorf("文案里应带上出错的条目名，实际 %q", text)
			}
		})
	}
}

// TestExportTextsUseGlossaryWhenLoaded 词库装载后应取到词库里的真译，而不是兜底。
//
// 这条是上一条的反面：只验「没有裸 key」会放过「永远走兜底」这种半坏状态 ——
// 词条在库里、代码却不查库，表现和正常一样，但英文界面会一直显示中文兜底。
func TestExportTextsUseGlossaryWhenLoaded(t *testing.T) {
	const key = "admin.media.variantStatus.pending"
	i18n.InjectForTest(
		map[string]map[string]string{
			key: {"zh-CN": "词库待处理", "en-US": "Queued from glossary"},
		},
		nil,
	)
	t.Cleanup(func() { i18n.InjectForTest(nil, nil) })

	if got := variantStatusText("pending", "zh-CN"); got != "词库待处理" {
		t.Errorf("zh-CN 应取词库译文，实际 %q", got)
	}
	if got := variantStatusText("pending", "en-US"); got != "Queued from glossary" {
		t.Errorf("en-US 应取词库译文，实际 %q", got)
	}
	// 词库里没这条时回落到硬编码兜底（不是空、不是裸 key）。
	if got := variantStatusText("failed", "en-US"); got == "" || strings.Contains(got, "admin.media.") {
		t.Errorf("未命中的 key 应回落到兜底文案，实际 %q", got)
	}
}
