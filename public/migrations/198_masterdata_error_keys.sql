-- 198 · masterdata 模块文案 key 化（审计 CQ-010 收尾）。
--
-- 背景：CQ-010 把 handler 的错误出口换成 response.ErrorAuto 后，IsBusinessError 只认两种
-- **形态确定**的判据 —— key 形态 `模块.类别.语义`（已迁模块）与 enums 常量名形态 `ErrXxx` / `MsgXxx`。
-- 而本模块的 enums 值本身是中文文案（ErrInvalidParam = "参数不合法"），两种都不匹配：
-- 它的全部业务错误因此被判成内部错误，对外统一 500 + 通用文案 —— 与「业务错误透出消息」相反。
--
-- 本迁移把 7 个常量（6 个 Err + 1 个 Msg）的值改成 key，文案落到本表（zh-CN / en-US 各 7 条）。
-- 迁完之后 pkg/response 里那层「纯文案 → 业务错误」的启发式判据被整层删除：判据回到确定性形态，
-- 不再依赖一份永远不全的内部错误特征清单去猜两种语义（见 response.go 的注释）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源，
-- 运营在后台改过的词条不会被下一次迁移覆盖。
--
-- 与 179（cart）/ 180（order）等模块的词条 seed 同形：列顺序、category、status、http_code 一致，
-- 只把 category 换成 'masterdata'。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
    ('masterdata.err.invalidParam', 'zh-CN', '参数不合法', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.invalidParam', 'en-US', 'Invalid parameter', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.entityTypeInvalid', 'zh-CN', '实体类型不在变更记录的白名单内', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.entityTypeInvalid', 'en-US', 'Entity type is not in the master data change whitelist', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.actionInvalid', 'zh-CN', '变更动作不合法', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.actionInvalid', 'en-US', 'Invalid change action', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.entityIDRequired', 'zh-CN', '实体 id 必填', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.entityIDRequired', 'en-US', 'Entity id is required', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.projectRequired', 'zh-CN', '未指定工程', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.projectRequired', 'en-US', 'No project specified', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.timeRangeInvalid', 'zh-CN', '时间范围不合法', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.err.timeRangeInvalid', 'en-US', 'Invalid time range', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.msg.listSuccess', 'zh-CN', '查询成功', 'masterdata', '', 1, 200, now(), now()),
    ('masterdata.msg.listSuccess', 'en-US', 'Query successful', 'masterdata', '', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
