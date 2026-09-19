package text

import "go_wp/internal/builder/core"

// 富文本清洗实现已上移到 core（internal/builder/core/richtext.go）：
// card / quote / infobox / faq 等内容字段与 core.text 共用同一套白名单，
// 禁止在组件内复制白名单。此处保留包内私有别名，text 包内部调用点与
// 既有 sanitize 测试（sanitize_test.go / sanitize_fuzz_test.go）名字不变。
//
// 规范：docs/02-C2 §2 编辑器能力；标题 h1~h5 原样保留
//（「h1 统一降级为 h2」的旧约定已取消，见 core/richtext.go 的 SanitizeRichHTML 注释）。

// sanitizeRichHTML 富文本白名单清洗（转发 core.SanitizeRichHTML）。
func sanitizeRichHTML(src string) string { return core.SanitizeRichHTML(src) }

// stripRichTags 提取富文本纯文本内容（转发 core.StripRichTags，摘要模式使用）。
func stripRichTags(src string) string { return core.StripRichTags(src) }
