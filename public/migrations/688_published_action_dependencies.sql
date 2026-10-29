-- 显式 Page/Presentation 动作沿既有依赖投影登记，不新增平行引用表。
ALTER TABLE page_dependencies DROP CONSTRAINT IF EXISTS page_dependencies_dependency_kind_check;
ALTER TABLE page_dependencies ADD CONSTRAINT page_dependencies_dependency_kind_check CHECK (dependency_kind IN (
 'direct_content','content_collection','content_template','menu','media','global_component','site_setting','runtime',
 'i18n','block','site_slot','navigation','page_target','presentation_target'
));
ALTER TABLE presentation_dependencies DROP CONSTRAINT IF EXISTS presentation_dependencies_dependency_kind_check;
ALTER TABLE presentation_dependencies ADD CONSTRAINT presentation_dependencies_dependency_kind_check CHECK (dependency_kind IN (
 'direct_content','content_collection','content_template','menu','media','global_component','site_setting','runtime',
 'i18n','block','site_slot','navigation','page_target','presentation_target'
));
