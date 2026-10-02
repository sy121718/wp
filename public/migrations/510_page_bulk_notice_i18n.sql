-- 510 · 页面管理页批量操作的受控回执词条（FIX-24 顺带裁决落地）。
--
-- 背景：批量操作的回执改成 shell.FacingNoticeSpec 的受控形状（URL 只带白名单 key + 有上限的计数，
-- 句子由词条决定）。旧形态的计数是客户端可改的 —— 读侧把模板归一化后比对（数字换成占位符），
-- 于是 ?done=已删除 999999 个页面 与真回执长得一样，运营分不出真假。
--
-- 词条必须与本批同批 seed：受控回执的读侧用 TranslateFor(c)(key, fallback) 取词，
-- 不 seed 则英文界面回落中文兜底（页面上的句子与界面语言不一致）。
-- 门禁 scripts/check-i18n-keys-seeded.sh 会检查「被引用的 key 是否已 seed」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改既有词条。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.pages.bulk.nothing', 'zh-CN', '批量操作：没有需要处理的页面。', 200, 'admin', '页面批量操作受控回执：无可处理项', 1, now(), now()),
('admin.pages.bulk.nothing', 'en-US', 'Bulk action: nothing to process.', 200, 'admin', 'page bulk notice: nothing to process', 1, now(), now()),
('admin.pages.bulk.deleted', 'zh-CN', '批量操作：已删除 {n} 个页面。', 200, 'admin', '页面批量操作受控回执：全成功', 1, now(), now()),
('admin.pages.bulk.deleted', 'en-US', 'Bulk action: deleted {n} page(s).', 200, 'admin', 'page bulk notice: all succeeded', 1, now(), now()),
('admin.pages.bulk.skipped', 'zh-CN', '批量操作：跳过 {m} 个页面（已不存在或无权限）。', 200, 'admin', '页面批量操作受控回执：全跳过', 1, now(), now()),
('admin.pages.bulk.skipped', 'en-US', 'Bulk action: skipped {m} page(s) (missing or not permitted).', 200, 'admin', 'page bulk notice: all skipped', 1, now(), now()),
('admin.pages.bulk.partial', 'zh-CN', '批量操作：已删除 {n} 个页面，跳过 {m} 个。', 200, 'admin', '页面批量操作受控回执：部分成功（契约要求的那条）', 1, now(), now()),
('admin.pages.bulk.partial', 'en-US', 'Bulk action: deleted {n} page(s), skipped {m}.', 200, 'admin', 'page bulk notice: partial success', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
