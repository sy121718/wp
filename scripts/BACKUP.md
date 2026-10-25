# 备份与恢复

## 为什么有这套脚本

发布路径有产物级回滚（`Publisher.Rollback`：把激活链接指回上一个产物），但**数据库没有**。
`public/migrations` 里有近 500 个 SQL 文件、500+ 条语句，一条迁移写错了 schema，产物回滚救不了
—— 唯一出路是外部备份。

同类项目最常见的失效不是「没有备份」，而是「备份从来没被恢复过」：文件在、校验和过、但
`pg_restore` 一跑就炸（缺扩展、权限不对、版本差异）。所以这里的核心不是 `pg_dump`，
而是 `restore.sh --drill`。

## 备份

```bash
bash scripts/backup.sh
# → .backup/<时间戳>/{db,site,manifest.env,SHA256SUMS}
```

| 内容 | 为什么在里面 |
|---|---|
| `db/<库名>.dump` | 全库（`-Fc`，支持选择性/并行恢复） |
| `db/row_counts.tsv` | 每表行数快照 —— 恢复后逐表对账用 |
| `db/pg_dump.log` | 失败时要看的原文 |
| `site/artifacts.tar.gz` | 站点产物 + **激活符号链接**（`public/runtime`） |
| `site/storage.tar.gz` | 上传的媒体资产（无产物可重建，丢了就是丢了） |
| `manifest.env` | 版本、提交、**迁移台账版本**、各路径 |
| `SHA256SUMS` | 恢复前先校验完整性 |

环境变量：`DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME`（也接受 `GOWP_` 前缀）、
`ARTIFACT_ROOT`、`STORAGE_DIR`、`BACKUP_DIR`。

打包用 `tar -h`：`public/runtime/public/active` 下全是指向 `artifacts/{hash}` 的符号链接，
`-h` 把目标内容一起打包（激活状态与产物必须同时存在才有意义）。**悬空链接**（取消发布后
旧产物已清理）会让 tar 非零退出，脚本会报告条数后继续 —— 不静默跳过，因为「激活链接少了几条」
正是恢复后 404 的原因。

## 恢复演练（上线前必做，也可以定期跑）

```bash
bash scripts/restore.sh --drill .backup/<时间戳>
```

流程：校验校验和 → 建临时库 → `pg_restore --exit-on-error --single-transaction` →
**逐表对账行数** → 删临时库。全程不碰生产库。

对账的判据不是「恢复没报错」，而是：
- 备份里有的表，恢复后都在；
- 备份时有数据的表，恢复后不是空的。

任一条不满足即非零退出并列出差表。

## 灾难恢复

```bash
# 1) 建一个空库（脚本不替你 DROP 生产库 —— 那个决定必须由人来做）
createdb -h <host> -U <user> wp_restored

# 2) 恢复到它
bash scripts/restore.sh --to wp_restored .backup/<时间戳>

# 3) 站点资产
bash scripts/restore.sh --site .backup/<时间戳>

# 4) 核对 manifest 里的 migration_head 与代码里的迁移版本一致
cat .backup/<时间戳>/manifest.env
```

站点资产恢复后脚本会逐个检查激活符号链接是否指向存在的产物，悬空即失败（否则站点会 404
而没有任何报错）。

## 定期化建议

部署机上一条 cron 足够：

```cron
# 每天 03:17 备份，保留 14 天
17 3 * * * cd /srv/go_wp && DB_USER=wp DB_NAME=wp BACKUP_DIR=/srv/backups bash scripts/backup.sh && \
           find /srv/backups -maxdepth 1 -mtime +14 -type d -exec rm -rf {} +
# 每周日 04:00 演练一次（证明这些备份真的能恢复）
0 4 * * 0 cd /srv/go_wp && DB_USER=wp BACKUP_DIR=/srv/backups bash scripts/restore.sh --drill "$(ls -d /srv/backups/*/ | tail -1)"
```

**没跑过 `--drill` 的备份不算备份。**
