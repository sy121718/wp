-- 显式动作与真实Publication已接管全部活跃消费者；仅清退旧唯一业务槽位。
DELETE FROM page_dependencies WHERE dependency_kind = 'site_slot';
DELETE FROM presentation_dependencies WHERE dependency_kind = 'site_slot';
ALTER TABLE page_dependencies DROP CONSTRAINT IF EXISTS page_dependencies_dependency_kind_check;
ALTER TABLE page_dependencies ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN (
 'direct_content','content_collection','content_template','menu','media','global_component','site_setting','runtime',
 'i18n','block','navigation','page_target','presentation_target'
));
ALTER TABLE presentation_dependencies DROP CONSTRAINT IF EXISTS presentation_dependencies_dependency_kind_check;
ALTER TABLE presentation_dependencies ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN (
 'direct_content','content_collection','content_template','menu','media','global_component','site_setting','runtime',
 'i18n','block','navigation','page_target','presentation_target'
));
DROP TABLE IF EXISTS page_site_slots;
