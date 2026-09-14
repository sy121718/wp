-- VIS-011：blocks.kind 与服务层白名单对齐，防止绕过 API 直写库。
ALTER TABLE blocks DROP CONSTRAINT IF EXISTS blocks_kind_check;
ALTER TABLE blocks ADD CONSTRAINT blocks_kind_check CHECK (
    kind IN (
        'block', 'header', 'footer',
        'announcement', 'sidebar', 'breadcrumb', 'drawer', 'search',
        'cta', 'trust', 'brands', 'contact', 'about',
        'banner', 'grid', 'snippet'
    )
);
