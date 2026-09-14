#!/usr/bin/env python3
"""批量将 audit finding 标为 resolved 并写入 resolutionNote。"""
import json
import glob
import os

ROOT = os.path.join(os.path.dirname(__file__), "..", "docs", "audit")

RESOLVED = {
    "SEC-001": "2026-09-13：无券时 discount 强制 0，忽略客户端 discountTotal。",
    "TX-001": "2026-09-13：order_expire 30min 超时取消 + 释库存/券；结算幂等 request_id 重放。",
    "TX-002": "2026-09-13：每人限次移入 redeemCouponTx（LockByIDTx + CountRedemptionsTx）。",
    "TX-003": "2026-09-13：RequestReturn 事务 + LockByIDTx + returnableByItemTx 锁内重算。",
    "TX-004": "2026-09-13：order_money 行级分摊 + lineRefundAmount；单测覆盖。",
    "TX-005": "2026-09-13：IsUniqueViolation 抽到 pkg/database；建单/退货/入库幂等重放。",
    "TX-006": "2026-09-13：已取消/已退款支付回调记流水 NeedsManualReview，不报错。",
    "TX-007": "2026-09-13：采购入库 ChangeStock 前查 ExistsMovementBySource 防重复加库存。",
    "TX-008": "2026-09-13：退货入库后 UpdateItemReceivedTx 失败向上返回可重试。",
    "TX-010": "2026-09-13：refundReturn 全额判定在 LockByIDTx 事务内完成。",
    "PERF-001": "2026-09-13：InvalidateKeys 改 go RebuildStale 异步；SetSyncRebuild 供测试。",
    "REG-001": "2026-09-13：删除 builder.go 双清单空导入，jetview.go 为唯一 hub。",
    "REG-002": "2026-09-13：TestComponentRegistryComplete 断言 len(core.Types())。",
    "UIK-001": "2026-09-13：uiBlocks 补 drawer/confirm/colorfield/iconfield/themetoggle；toast/busy 仅后台 API。",
    "UIK-014": "2026-09-13：button 改 data-modal-open，与 modal.js 一致。",
    "UIK-006": "2026-09-13：ReducedMotionCSS 无条件输出 prefers-reduced-motion 块。",
    "EDT-002": "2026-09-13：迁移 159 默认 article 详情模板种子（heading/image/text）。",
    "EDT-003": "2026-09-13：pipeline 统一装配；presentation 对齐六类构建期能力。",
    "EDT-015": "2026-09-13：preview/publish 共用 SiteCompileOptions；compile_parity_test。",
    "I18N-011": "2026-09-13：Request.Lang/T、Vary、compile_site 槽位按语言。",
    "I18N-012": "2026-09-13：片段 Go/jet 全量 site.fragment.* + 迁移 156–158。",
    "I18N-013": "2026-09-13：publishAllLangs + 155 迁移；presentation_i18n + 双语测试。",
    "SEO-001": "2026-09-13：seo/locale.go CJK/英文分词；ScoreDocument(lang)。",
    "SEO-008": "2026-09-13：compile_site LocaleView WithAlternates；presentation 多语言 hreflang。",
    "DB-011": "2026-09-13：page validateKind 移除 product/category；enums.ValidatePageContentContract。",
    "DB-012": "2026-09-13：blueprint PageKinds 委托 page/enums 白名单。",
    "OSS-009": "2026-09-13：根目录添加 MIT LICENSE。",
    "OSS-010": "2026-09-13：.github/workflows/go-test.yml unit + PG/Redis integration job。",
    "OSS-011": "2026-09-13：SECURITY.md；GitHub Issue/PR 模板待补。",
    "SEC-006": "2026-09-13：本地上传 filepath.Rel 根目录 containment 断言。",
    "SEC-008": "2026-09-13：release 模式 session_secret 最短 32 字符 fail-fast。",
    "SEC-012": "2026-09-13：HTTP 日志 query 敏感参数 redact。",
    "SEC-016": "2026-09-13：添加 SECURITY.md 私下报告渠道说明。",
    "VIS-004": "2026-09-13：block RequireWiring fail-fast；referenced nil 宁拒勿删。",
    "VIS-003": "2026-09-13：RefreshStructureForTheme 逐页 MergeStructureBindings，页面 override 保留。",
    "VIS-007": "2026-09-13：page model 注释明确 theme_id 为工程级冗余快照。",
    "SEO-005": "2026-09-13：ProductOfferLD + buildJSONLD offers/rating；presentation_seo 注入。",
    "DB-013": "2026-09-13：迁移 160 解除 entity_type CHECK；category→product_category。",
    "SEC-002": "2026-09-13：page/order/product/block/media 按 id 查询强制 projectId 过滤。",
    "IDX-001": "2026-09-13：迁移 161 + PurgeExpiredViews 日调度；默认保留 90 天。",
    "IDX-003": "2026-09-13：迁移 161 masterdata_retention_days 配置列（分区归档留 DB-004）。",
    "CQ-011": "2026-09-13：pkg/database/like.go EscapeLikePattern 统一四处 LIKE 转义。",
    "PERF-003": "2026-09-13：订单 List Omit attribution 列。",
    "PERF-005": "2026-09-13：RefreshOnline TTL/3 节流。",
    "I18N-020": "2026-09-13：endpoint SitePagesOf 使用 Request.Lang。",
    "TX-015": "2026-09-13：迁移 134 注释说明 order_items 外键有意排除。",
    "OSS-021": "2026-09-13：AGENTS.md 数据库连接说明去硬编码。",
    "UI-010": "2026-09-13：admin 模板 confirm() 改 data-confirm（10 处）。",
    "UIK-012": "2026-09-13：theme.css reset 收窄，排除 dialog 居中。",
    "EDT-010": "2026-09-13：editor_contract.go 注释澄清命名与 ComponentSchemas 分工。",
    "EDT-011": "2026-09-13：docs/03-A-workbench.md 与实现对齐。",
    "IDX-007": "2026-09-13：迁移 162 inventory_stock_movements (warehouse_id, created_at DESC)。",
    "DB-016": "2026-09-13：page_site_slot_test 对齐 migration 138 CHECK。",
    "SEC-014": "2026-09-13：upload 拒绝 SVG + /storage nosniff。",
    "SEC-010": "2026-09-13：购物车 cookie v2 含签发时间校验。",
    "TX-013": "2026-09-13：定价 margin 上限 0.95。",
    "PERF-012": "2026-09-13：artifact ReplaceArtifactContent CreateInBatches。",
    "I18N-023": "2026-09-13：docs/06-D-site-i18n.md 更新实现状态。",
    "I18N-025": "2026-09-13：docs/06-D §12.1 区分 mail 与站点 i18n。",
    "OSS-015": "2026-09-13：docs/api-stability.md API 稳定性分级。",
    "OSS-020": "2026-09-13：docs/09-session-handoff.md 去绝对路径。",
    "REG-003": "2026-09-13：docs/02-C0 明确内置组件不走 json/yaml 注册。",
    "PERF-021": "2026-09-13：registry_version.go 注释说明双指纹设计。",
    "DB-014": "2026-09-13：docs/schema-snapshot.md 说明 init 为历史快照。",
    "DB-017": "2026-09-13：迁移 081/099/102 注释去除库存缓存过时描述。",
    "DB-022": "2026-09-13：docs/schema-snapshot.md 归纳 Go 注册表 vs DDL CHECK。",
    "DB-023": "2026-09-13：docs/schema-snapshot.md tag/pricing 规则校验分层。",
    "VIS-009": "2026-09-13：docs/02-D § 站点骨架 kind/structure/navigation 关系。",
    "CQ-024": "2026-09-13：清理 product/inventory/dashboard 过时注释。",
    "CQ-016": "2026-09-13：CancelOrder 库存归还失败返回 Warnings + 流转留痕，不再静默 nil。",
    "CQ-017": "2026-09-13：RevokeByTokenHash 失败记 Error 日志，不再 _, _ 忽略。",
    "CQ-018": "2026-09-13：删页 SyncReferences 失败中断删除，避免引用残留。",
    "DB-002": "2026-09-13：migrator 启动 pg_try_advisory_lock 互斥多实例 DDL。",
    "DB-003": "2026-09-13：compareVersion 按版本前缀数值排序，规避字符串序陷阱。",
    "EDT-001": "2026-09-13：/workbench?template= MVP + /admin/content-templates 列表与编辑入口。",
    "I18N-004": "2026-09-13：LanguageOptions 取自 i18n.AvailableLangs()，随词条 seed 扩展。",
    "I18N-005": "2026-09-13：dashboard enums 标题 key + withI18n 翻译；迁移 163 seed。",
    "I18N-016": "2026-09-13：站点设置 LangURLOffWarning + MsgSiteLangURLOffWarning 提示。",
    "I18N-022": "2026-09-13：语言根 /index；CanonicalPublicPath + SiteRedirect 301 到 /。",
    "PERF-010": "2026-09-13：menu_cache 30s TTL；BuildAuthorizedTree 走 listAllMenusCached。",
    "SEO-002": "2026-09-13：keywordDensity 按 locale CJK 字符/英文词数；短词 CJK 提示。",
    "SEO-003": "2026-09-13：缺 SizeKB 时体积项 -1 不纳入评分，不再恒 0 分。",
    "SEO-004": "2026-09-13：EvaluateSchemaPresence 对齐 buildJSONLD；文章侧 schemaType=article。",
    "SEO-010": "2026-09-13：面包屑首项 site.breadcrumb.home 构建期 i18n（非写死 Home）。",
    "SEO-017": "2026-09-13：chkTitleLength 按 CJK 双宽展示单位 40–60 满分（scoring_test）。",
    "SEO-022": "2026-09-13：page/presentation 发布 notifyIndexNow；settings.indexNowKey 配置。",
    "TX-012": "2026-09-13：DeleteVariant 非零库存拒绝（ErrVariantHasStock）再 CASCADE。",
    "TX-014": "2026-09-13：content_objects INSERT ON CONFLICT DO NOTHING 消并发 TOCTOU。",
    "UI-004": "2026-09-13：button/socialbuttons hover 改 @hover 桶；触屏不输出裸 :hover。",
    "UI-007": "2026-09-13：wb-media-panel 宽 min(calc(100vw-32px),1280px)，窄屏不溢出。",
    "UI-013": "2026-09-13：节点操作栏 is-selected/focus-within 常显；hover 仅 @media (hover:hover)。",
    "VIS-005": "2026-09-13：ReskinProjectForTheme 单事务换皮；激活后失败返回 500。",
    "OSS-014": "2026-09-13：config GOWP_ AutomaticEnv 环境变量覆盖 YAML。",
    "PERF-009": "2026-09-13：collection_resolver translateFieldsBatch 批量查译文。",
    "PERF-018": "2026-09-13：database statement_timeout + conn_max_idle_time 可配。",
    "EDT-018": "2026-09-13：商品列表预览详情页 + 文章编辑页链到详情模板。",
    "EDT-014": "2026-09-13：迁移 164 is_default；ResolveTemplate 优先默认模板。",
    "UI-017": "2026-09-13：carticon/orderlist 宽度 min(100%, Npx)。",
    "UI-018": "2026-09-13：badge/订单状态 role=status + aria-label。",
    "CQ-014": "2026-09-13：pkg/utils/paging Normalize 四处复用。",
    "DB-015": "2026-09-13：迁移 165 orders.status CHECK 约束。",
    "EDT-012": "2026-09-14：集合项 url 由 PublishedEntityPaths 填入；palette 默认 linkField=item.url。",
    "EDT-005": "2026-09-14：entityref 控件 + 检查器分类/品牌/标签下拉（CollectionFilterOptions）。",
    "PERF-007": "2026-09-14：SiteCacheMiddleware 为 /site 下发 must-revalidate。",
    "SEC-003": "2026-09-14：访客注册/登录/重置、analytics/collect、/_fragments 专项限流。",
    "VIS-011": "2026-09-14：迁移 166 blocks.kind CHECK 与服务层白名单对齐。",
    "PERF-002": "2026-09-14：productList/searchResults GET 片段 Redis 短 TTL 缓存 + 商品写后版本失效。",
    "PERF-004": "2026-09-14：ListForCollection 评分改 SQL 聚合子查询，移除 Ratings Preload。",
    "CQ-026": "2026-09-14：ApplyPricing 主数据审计改 RecordChangesTx 与改价同事务。",
    "EDT-016": "2026-09-14：RenderNodeHTML 使用 DiscardCSS，片段路径跳过 CSS 编译。",
    # ---- 2026-09-14 第二批（P0 收口）----
    "SEC-004": "2026-09-14：debug/test 监听地址默认收紧到 127.0.0.1（server.debug_allow_public 显式放开并告警）；所有非 release 响应带 X-GOWP-Debug 标记头；README 补生产部署检查清单。dev_login 的环回校验本就取 RemoteAddr（不受 XFF 影响）。",
    "SEC-007": "2026-09-14：新增 pkg/sitehttps 作为「站点是否 HTTPS」的单一判定（server.site_https 显式 > server.mode 推导 > 未初始化 fail-closed）；auth / response / runtimefragment 三处 Secure 统一走它，不再采信 X-Forwarded-Proto。",
    "SEC-009": "2026-09-14：public/test/support/dto_exposure.go —— AST 扫描全部 dto 包 + 反射断言（共用敏感词与允许清单，允许项须写理由）；public/test/security/unit 覆盖 mail/admin/user 关键响应类型；客户管理原有断言收敛到该实现。",
    "TX-016": "2026-09-14：public/test/order/feature/order_concurrency_test.go —— 并发发货/并发支付/取消与支付并发/并发退货/同用户并发用券五条，全部 -race 通过（验收 TX-002、TX-003、TX-006 的修复）。",
    "CQ-023": "2026-09-14：CreateOrder 拆为 order_create.go（编排+校验+幂等）/ order_create_draft.go（快照·金额·券·归因·访客开号）/ order_create_persist.go（事务写入·扣库存·补偿）；新增 order_create_draft_test.go 覆盖入参校验、折扣计算、券判定、分摊边界。",
}


def patch_file(path: str) -> int:
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    n = 0
    for finding in data.get("findings", []):
        fid = finding.get("id")
        if fid in RESOLVED and finding.get("status") != "resolved":
            finding["status"] = "resolved"
            finding["resolutionNote"] = RESOLVED[fid]
            n += 1
    if n:
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)
            f.write("\n")
    return n


def main():
    total = 0
    for path in sorted(glob.glob(os.path.join(ROOT, "dimensions", "*.json"))):
        c = patch_file(path)
        if c:
            print(f"{os.path.basename(path)}: {c}")
            total += c
    print(f"total marked resolved: {total}")


if __name__ == "__main__":
    main()
