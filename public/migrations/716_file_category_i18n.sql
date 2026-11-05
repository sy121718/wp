-- 716 · 文件分类（file category）与媒体列表游标的错误词条
--
-- 背景：`internal/module/media/service/media.go` 里这七条错误此前是
--   `errors.New("中文串")` —— 既不进词库、也不带状态码，出口只能把它们当成内部错误
--   给一句归口文案，用户看不出到底哪一步错了。现在它们改成 enums 里的业务错误声明
--   （key + 状态码），文案归这里。
--
-- 为什么前缀是 FileCategory 而不是 Category：product 域已经有一套 ErrCategory*
--   （商品分类，见 430_i18n_enums_missing_keys.sql:133-148）。词库的 (item_key, lang)
--   是全局唯一键，共用同名 key 就等于两个模块共享同一句文案的所有权 —— product 改一次
--   措辞，媒体库的分类操作会跟着变，而且没人知道为什么。media 这边管的是另一张表
--   （file_categories），所以 key 也自成一套，与表名对齐，天然不撞。
--
-- 文案口径：按**用户看到的语义**写，不是把 Go 里那句内部提示语搬过来（「分类不存在」
--   这种说法在文件分类场景下要讲清楚是「文件分类」，否则用户会以为是商品分类）。
--   「父级不能是自己」与「不能移动到自己的子分类下」共用 ErrFileCategoryCycle 一条：
--   两者是同一失败原因（成环）的特例与一般形。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见
--   register_file_category_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrFileCategoryNameRequired', 'zh-CN', '请填写文件分类名称', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNameRequired（media.go:360 分类名称不能为空）', 1, now(), now()),
('ErrFileCategoryNameRequired', 'en-US', 'Enter a file category name.', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNameRequired (media.go:360, empty category name)', 1, now(), now()),

('ErrFileCategoryNotFound', 'zh-CN', '文件分类不存在，可能已被其他管理员删除', 404, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNotFound（media.go:54 / :402 / :470 / :500）', 1, now(), now()),
('ErrFileCategoryNotFound', 'en-US', 'This file category does not exist. Another administrator may have deleted it.', 404, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNotFound (media.go:54 / :402 / :470 / :500)', 1, now(), now()),

('ErrFileCategoryParentNotFound', 'zh-CN', '选定的上级文件分类不存在', 404, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryParentNotFound（media.go:364 / :433）', 1, now(), now()),
('ErrFileCategoryParentNotFound', 'en-US', 'The selected parent file category does not exist.', 404, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryParentNotFound (media.go:364 / :433)', 1, now(), now()),

('ErrFileCategoryNameDuplicated', 'zh-CN', '同一上级下已有同名的文件分类，请换一个名称', 409, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNameDuplicated（media.go:373 / :419）', 1, now(), now()),
('ErrFileCategoryNameDuplicated', 'en-US', 'A file category with this name already exists under the same parent. Choose another name.', 409, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryNameDuplicated (media.go:373 / :419)', 1, now(), now()),

('ErrFileCategoryCycle', 'zh-CN', '不能把文件分类挂到自身或自己的下级分类下', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryCycle（media.go:429 父级不能是自己 / :441 不能移动到自己的子分类下）', 1, now(), now()),
('ErrFileCategoryCycle', 'en-US', 'A file category cannot be placed under itself or one of its own subcategories.', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryCycle (media.go:429 parent is self / :441 move under own descendant)', 1, now(), now()),

('ErrFileCategoryHasChildren', 'zh-CN', '该文件分类下还有子分类，请先删除或移走子分类', 409, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryHasChildren（media.go:477）', 1, now(), now()),
('ErrFileCategoryHasChildren', 'en-US', 'This file category still has subcategories. Delete or move them first.', 409, 'media', 'internal/module/media/enums/media_enums.go: ErrFileCategoryHasChildren (media.go:477)', 1, now(), now()),

('ErrMediaCursorInvalid', 'zh-CN', '翻页游标已失效，请重新进入媒体库列表', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrMediaCursorInvalid（media.go:204 / :208 / :212 / :216）', 1, now(), now()),
('ErrMediaCursorInvalid', 'en-US', 'This page cursor is no longer valid. Open the media library again.', 400, 'media', 'internal/module/media/enums/media_enums.go: ErrMediaCursorInvalid (media.go:204 / :208 / :212 / :216)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
