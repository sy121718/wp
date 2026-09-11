# media + project 模块代码审查报告（37 文件 / 4102 行）

## P0 严重
### 1. UpdateAttachment 读-改-写整体覆盖 extra_info，并发构建时丢失 refs 引用缓存（竞态/数据损坏）
- 位置：internal/module/media/service/media_category.go:153-206
- 证据（L154 读快照 / L180 反序列化旧值 / L204 整体写回）：
  e, err := s.am.GetByID(ctx, req.ID)
  if e.ExtraInfo != nil && *e.ExtraInfo != "" { json.Unmarshal([]byte(*e.ExtraInfo), &extra) ... }
  raw, err := json.Marshal(extra); updates["extra_info"] = string(raw)
- 问题：refs 写入侧（media_ref_model.go:79-137 的 AddRef/ReplaceRefs）是单条 jsonb_set SQL 原子更新，但 UpdateAttachment 是 GetByID 读快照 → 改 alt/title → 整体 Marshal 写回。GetByID 与 Updates 之间若页面构建任务 AddRef 写入了 refs（page_assemble.go:69 构建期调用），旧快照会把新 refs 覆盖掉 → 引用缓存丢失 → 删除保护失效（被线上页面引用的图片可被删除）且无法自愈。media_ref_model.go:3-11 文件头注释明确声称「全部更新走单条 SQL，不做读-改-写，构建期与后台元数据编辑并发时各自只影响自己的键」——与 UpdateAttachment 实现直接矛盾。
- 建议：alt/title/description 改为单条 SQL jsonb_set 逐键写入（照 media_ref_model 写法），或对附件行 SELECT FOR UPDATE 后再合并写回。

## P1 高
### 2. /api/media/replace 与 /api/media/references 无权限点 seed，Casbin 精确匹配下全员 403，换图与引用查询功能死链
- 位置：internal/module/media/inbound/http/media_router.go:34-35
- 证据：g.POST("/replace", handle.Replace); g.GET("/references", handle.References)
- 问题：路由挂在 authorizedAPI（SessionAuth+CSRF+Casbin，routes.go:125）。Casbin matcher 为精确等值（pkg/casbin/casbin.go:74：r.obj == p.obj && r.act == p.act），无通配、无超管 bypass；超管策略由 051 seed 从 sys_permission 全表 CROSS JOIN 生成。而全部迁移中（030 只有 list/detail/upload/update/delete/category_*；048 只有 download/download_batch/variants_generate；067 只加列不加权限点）均无 media:replace / media:references 权限点 → Enforce 恒 false → 包括超管在内所有用户 403。换图是 067 媒体中心核心能力，且无 dashboard 替代入口（全仓 grep 确认 svc.Replace/svc.References 仅 media_handle.go:116,131 调用）。
- 建议：新增 seed 补 media:replace（POST /api/media/replace）与 media:references（GET /api/media/references）两条权限点（照 048 写法），重跑 051 补超管策略。

## P2 中
### 3. ActivateTheme 事务第二步 UPDATE 目标行不匹配时静默成功，工程落入「无激活主题」
- 位置：internal/module/project/model/theme_model.go:77-86；service theme_service.go:152-164
- 证据：tx.Model(&ThemeEntity{}).Where("id = ? AND project_id = ?", themeID, projectID).Updates(...).Error // L83-84 不检查 RowsAffected
- 问题：事务先全量取消激活再激活目标。若目标主题在 service GetTheme（L156）之后被并发删除，第二步 UPDATE 匹配 0 行不报错，事务提交成功 → API 返回成功但全工程 is_active 全 false。「激活成功」与实际状态不符。
- 建议：检查第二步 RowsAffected，为 0 时 return error 回滚，service 映射 ErrThemeNotFound。

### 4. 页面软删除不清理媒体引用缓存，refs 永久残留导致附件删除被永久误拦（跨模块协作缺口）
- 位置：media_ref.go:49-96（SyncReferences 是清空唯一通道）；page/service/page_delete.go:20-39 未调用
- 证据：refs 唯一写入点是构建期 page_assemble.go:69 SyncReferencesFromHTML(ctx, "page", pageID, ...)；page_delete.go 只清路由 + 软删页面。
- 问题：页面删除后产物已下线，但附件 extra_info.refs 里 kind=page,id=<已删页> 残留；该页 ID 永不再构建 → refs 永不清除 → Delete（media_crud.go:203-212）「被 N 个页面引用」拦截永久误报，附件无法删除。
- 建议：page 删除编排追加 media.SyncReferences(kind="page", refID=页ID, URLs=[]) 清空；或提供「重建引用缓存」运维入口。

### 5. AddRef 为 check-then-act，并发构建同一附件可写入重复 refs 条目
- 位置：internal/module/media/model/media_ref_model.go:79-103
- 证据：L80 HasRef 查询与 L99 Update 之间无锁、Update 无「refs 不含该项」守卫。
- 问题：两个页面并发构建且引用同一张图时双方 HasRef 都 false，各自执行 || payload 追加 → refs 数组重复项（summarizeRefs 计数虚高）。与文件头「全部更新走单条 SQL」的原子性声明不符（追加原子，判重不原子）。
- 建议：判重并入 UPDATE 守卫（WHERE NOT extra_info @> ?::jsonb）+ RowsAffected 判定。

### 6. Replace 换图：rename 覆盖目标文件后 DB 回填失败，md5/file_size 与磁盘内容永久不一致
- 位置：internal/module/media/service/media_replace.go:106-123
- 证据：L106 os.Rename 已覆盖目标；L111 IncrementGeneration、L116 AttachmentUpdate 任一失败直接 return err。
- 问题：文件已是新内容但 md5/大小/MIME 仍旧值 → 后续同内容上传命中「内容未变」幂等分支返回错误现状；去重键失真；generation 与内容代数错位（依赖重建判定失真）。无自愈路径。
- 建议：回填失败时降级为后台补偿任务并记录；md5 与 generation 合并单条 SQL 缩小窗口。

### 7. UpdateCategory 同时改父级+改名时重名查重用旧父级，新父级下重名漏检
- 位置：internal/module/media/service/media_category.go:72-84（L80 用 current.ParentID 旧父级），移动分支 L89-107 不复查新父级重名
- 问题：一次请求同时传 category_name + parent_id 时，重名校验按旧父级做，移动后可与新父级下同名分类共存（无 DB 唯一约束兜底），破坏「同父级唯一名」约束。
- 建议：req.ParentID != nil 且 != current.ParentID 时，查重基准改用新父级。

### 8. SVG 上传链路自相矛盾：config.yaml 白名单允许 .svg/image/svg+xml，但嗅探层一票否决 SVG 内容
- 位置：config.yaml:113-114 + pkg/upload/upload.go:462-484（detectDangerousContent 拒 image/svg+xml / <svg / <?xml 前缀）+ media_crud.go:259-281（classifyType 把 .svg 归类 image）
- 问题：配置声明支持 SVG（分类、变体排除表 variantExcludedExts 均按 svg 处理），但嗅探层对 SVG 魔数一律拒绝 → 正常 SVG 上传必然失败；而以 <!-- 注释开头的非常规 SVG（DetectContentType 判 text/plain 且不匹配前缀）可绕过并从 /storage 直出——安全侧与功能侧均未对齐。
- 建议：二选一对齐：从 config.yaml 白名单移除 .svg 与 image/svg+xml；或提供 SVG 消毒通道（白名单标签/属性清洗）后再放行。

## P3 低
### 9. media handler 错误处理粗糙：业务错误 5xx、err.Error() 直出、错误的状态码语义
- 位置：media_handle.go:48-51（Upload 一律 500，含「目标分类不存在」业务错误，且 err.Error() 可能携带底层错误链细节）；64-67（List 500+err.Error()）；82-84/96-98/131-135（Detail/Delete/References 一律 404，「被引用拒绝」也 404）
- 问题：与 project 模块 projectError/themeError 兜底模式（500 固定文案+日志留原文）不一致；引用拒绝返回 404 会让前端误判不存在。
- 建议：补 error 映射函数：业务哨兵→400/403/409，基础设施故障→固定文案+日志。

### 10. Upload 的 category_id 解析失败被静默吞掉
- 位置：media_handle.go:41-46（ParseUint 失败时 categoryID 保持 nil 继续上传，无提示）
- 建议：解析失败直接 ParamError。

### 11. 上传去重并发 TOCTOU（md5+file_type 无唯一约束）
- 位置：media_crud.go:53-59（GetByMD5AndType 与 Create 间无锁）；init_schema.sql 确认 md5 无唯一索引
- 问题：并发上传同一文件产生两条附件记录（重复存储，无数据损坏）。可接受或加部分唯一索引+冲突改查复用。

### 12. DeleteCategory 的 DetachAttachments 与 DeleteCategory 两步无事务
- 位置：media_category.go:144-147（Detach 成功、Delete 失败 → 附件已移未分类但分类仍在；重试可恢复）。建议 Transaction 编排或注释接受幂等。

### 13. GenerateVariants 并发重入撞 UNIQUE(attachment_id, variant_type)，返回状态与 DB 脱节
- 位置：media_variant.go:235-237（先 DeleteByAttachment）+ 293-305（insertVariant 失败仅记日志返回 rec.ID=0）；048 迁移 L30 UNIQUE 约束
- 问题：异步任务与「重新生成」按钮并发时第二次 insert 报唯一冲突，insertVariant 返回 ID=0，后续 vm.Update(0) 更新 0 行，响应却宣称 ready。
- 建议：insert 失败返回 nil 跳过该变体输出，或改 UPSERT。

### 14. model 层软删除过滤缺失清单（sys_attachment，当前由 service 前置校验兜底）
- 位置：media_model.go:233-235（AttachmentUpdate 无 status 条件）；141-150（IncrementGeneration）；media_ref_model.go 全部方法（AddRef/RemoveRef/ListRefs/HasRef/ListIDsByRef 无 status 过滤，构建期入口被 GetByFilePath 的 status=1 挡住）
- 建议：统一加 status=1 守卫（ListIDsByRef 命中软删行无害可保留）。

### 15. attachmentIDByURL 死分支；ReplaceRefs 死代码
- 位置：media_ref.go:108-111（strings.Index >= 0 已涵盖 HasPrefix，else if 分支恒 false）；media_ref_model.go:124-137（ReplaceRefs 无调用方）

### 16. SessionAuthMiddleware 双重挂载，每请求双倍 Redis 会话校验/心跳
- 位置：media_router.go:25、project_router.go:20、theme_http.go:32（routes.go:125 authorizedAPI 已统一挂）

### 17. BuildBatchDownloadPlan 循环 N+1 且 ids 无上限
- 位置：media_download.go:213-227（循环 GetByID）；media_handle.go:255-269（parseIDList 无上限）
- 建议：数量上限（如 100）+ model 补 ListByIDs。

### 18. ProbeImageVariants srcset 宽度声明用固定边长而非实际宽度
- 位置：media_variant.go:365-385（w = thumbVariantEdge/mediumVariantEdge 常量）
- 问题：imaging.Fit 不放大，小于 320px 的源图 thumb 实际宽=原图宽，srcset 却声明 320w。建议 ready 变体取 v.Width 实际值。

### 19. project/theme/locale 细节问题
- theme_http.go:63,136 用驼峰 Query 参数 projectId（全项目 snake_case 惯例不一致）；theme_http.go:121-132 Delete 复用 ThemeActivateReq
- project_handle.go:30-32：Create 绑定失败（含 settings 非法）统一返回 ErrInvalidName 文案，误导排障
- theme_service.go:68-107 CreateTheme 不校验 ProjectID 存在（靠 FK 报 500）；locale_service.go:77-79 SaveLocales 不校验工程存在且 project_locales（064）无 FK → 可写孤儿语言清单
- theme_model.go:97-107 查重 LOWER(name) 而 DB 唯一索引 uq_themes_project_name（020:16）精确匹配 → 并发下可建 "Foo"/"foo" 两主题
- locale_service.go:42-47/60-64 EnabledLangs/DefaultLocale 吞一切查询错误（含 ctx 取消）回退默认语言（存疑：注释声明是为不阻断构建的有意设计，但会掩盖 DB 故障）

## 统计
- 审查文件数：37（media 20 个 2870 行 + project 17 个 1232 行），全部逐行读完；另交叉核对 routes.go、SessionAuth/CSRF/Casbin 中间件、pkg/upload、config.yaml 及 020/030/031/035/037/048/051/064/067/068 迁移
- 各级问题数：P0 = 1，P1 = 1，P2 = 6，P3 = 11（含 1 条存疑）
- 符合项确认（重点核查无问题）：LIKE 通配符转义正确（media_model.go:168-171，单遍 Replacer 顺序正确 + ESCAPE）；分类树防环（isDescendant）；删除媒体引用拦截逻辑本身正确；主题激活竞态有 037 部分唯一索引兜底；DeleteTheme 有 is_active=false 原子守卫防 TOCTOU；路由全 GET/POST 无路径参数；SQL 全参数化、context 全传播；model 层无跨模块调用、无业务规则下沉；跨模块全走 contract；project/theme settings 均校验必须为 JSON 对象（normalizeSettings 拒绝 null/数组/标量）无 JSONB 注入；zip 条目名 sanitizeZipName 防 zip-slip；localObjectPath Clean+根前缀校验防穿越；writeLocalFile O_TRUNC|O_NOFOLLOW；writeZipResponse 文件句柄显式 Close 无泄漏；locale 保存校验完整（至少一种语言/至多一个默认/默认必须启用/语言码白名单）