-- 060 · i18n 词条 seed（访客面组件固定文案 site.component.*，多语言 P4）
--
-- 覆盖：13 个 key / zh-CN 13 行 / en-US 13 行（构建期组件文案，中英均为人工编写，非机器伪造）。
-- 来源：
--   internal/templates/components/{gallery,slider,nav,video}.jet（模板内硬编码中文 → view 预翻译）
--   internal/builder/components/{countdown,form,rating}/*.go（Go 侧生成的访客可见文案）
-- 命名：site.component.{type}.{prop}（docs/06-D §10.3；site.* 为访客面命名空间）。
-- 占位符：仅 %s（与 pkg/i18n.HasStringPlaceholdersOnly 约定一致；rating 用两个 %s）。
-- 兜底：构建期缺词条时回退组件包内中文原文（core.RenderContext.Text），绝不输出空串或裸 key。
-- slide_label 为「模板型」词条（含单个 %s）：构建期下发到 data-slide-label，由 wp-enhance.js 按序号替换。
-- 语义（审计 I18N-003）：ON CONFLICT DO NOTHING —— seed 是**默认值来源**，不是真相来源。
--   后台改过的词条不会被下一次迁移覆盖（DO UPDATE 的旧写法会让运营的修改在下次部署时
--   静默回滚，而「我明明改过」这种问题极难定位）。要改默认值请改这里的 item_value 并删除
--   对应行后重跑，或在后台直接修改。
-- 幂等：ON CONFLICT (item_key, lang) DO UPDATE（可重复执行）；
--       注册见 register.go，ConditionSQL 以 site.component.* 的 zh-CN 行数 13 为门槛。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('site.component.gallery.prev', 'en-US', 'Previous', 200, 'ui', 'internal/templates/components/gallery.jet', 1, now(), now()),
('site.component.gallery.prev', 'zh-CN', '上一张', 200, 'ui', 'internal/templates/components/gallery.jet', 1, now(), now()),
('site.component.gallery.next', 'en-US', 'Next', 200, 'ui', 'internal/templates/components/gallery.jet', 1, now(), now()),
('site.component.gallery.next', 'zh-CN', '下一张', 200, 'ui', 'internal/templates/components/gallery.jet', 1, now(), now()),
('site.component.slider.prev', 'en-US', 'Previous', 200, 'ui', 'internal/templates/components/slider.jet', 1, now(), now()),
('site.component.slider.prev', 'zh-CN', '上一张', 200, 'ui', 'internal/templates/components/slider.jet', 1, now(), now()),
('site.component.slider.next', 'en-US', 'Next', 200, 'ui', 'internal/templates/components/slider.jet', 1, now(), now()),
('site.component.slider.next', 'zh-CN', '下一张', 200, 'ui', 'internal/templates/components/slider.jet', 1, now(), now()),
('site.component.slider.slide_label', 'en-US', 'Slide %s', 200, 'ui', 'internal/templates/components/slider.jet + internal/builder/enhance.js', 1, now(), now()),
('site.component.slider.slide_label', 'zh-CN', '第 %s 张', 200, 'ui', 'internal/templates/components/slider.jet + internal/builder/enhance.js', 1, now(), now()),
('site.component.nav.label', 'en-US', 'Site navigation', 200, 'ui', 'internal/templates/components/nav.jet', 1, now(), now()),
('site.component.nav.label', 'zh-CN', '站点导航', 200, 'ui', 'internal/templates/components/nav.jet', 1, now(), now()),
('site.component.video.title', 'en-US', 'Video', 200, 'ui', 'internal/templates/components/video.jet', 1, now(), now()),
('site.component.video.title', 'zh-CN', '视频', 200, 'ui', 'internal/templates/components/video.jet', 1, now(), now()),
('site.component.countdown.days', 'en-US', 'Days', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.days', 'zh-CN', '天', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.hours', 'en-US', 'Hours', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.hours', 'zh-CN', '时', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.minutes', 'en-US', 'Minutes', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.minutes', 'zh-CN', '分', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.seconds', 'en-US', 'Seconds', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.countdown.seconds', 'zh-CN', '秒', 200, 'ui', 'internal/builder/components/countdown/jet.go', 1, now(), now()),
('site.component.form.submit', 'en-US', 'Submit', 200, 'ui', 'internal/builder/components/form/jet.go', 1, now(), now()),
('site.component.form.submit', 'zh-CN', '提交', 200, 'ui', 'internal/builder/components/form/jet.go', 1, now(), now()),
('site.component.rating.label', 'en-US', 'Rated %s out of %s', 200, 'ui', 'internal/builder/components/rating/jet.go', 1, now(), now()),
('site.component.rating.label', 'zh-CN', '评分 %s / %s', 200, 'ui', 'internal/builder/components/rating/jet.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
