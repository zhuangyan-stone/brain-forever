# 修改初始化 SQL 规范

## 目录结构对应关系

项目使用两套目录存放 SQL 文件：

| 用途 | 模板目录（可编辑） | 部署目录（运行时读取） |
|------|-------------------|----------------------|
| 建表/DDL | `bin.template/settings_template/init_sql.template/` | `bin/settings/init_sql/` |
| 其他配置模板 | `bin.template/settings_template/` | `bin/settings/` |

- **模板目录**：存放 `.template.sql` 文件，是编辑修改的目标。
- **部署目录**：存放 `.sql` 文件（去掉 `.template` 后缀），是程序启动时实际读取的位置。

## 文件命名规则

所有 SQL 文件使用 **`nnn.xxx.sql`** 格式命名，其中：

- `nnn` — 三位数字序号，新文件的序号 = 当前最大序号 + 1
- `xxx` — 简短英文描述，用下划线分隔单词

### 模板文件 vs 部署文件

| 位置 | 命名格式 | 示例 |
|------|----------|------|
| `bin.template/settings_template/init_sql.template/` | `nnn.xxx.template.sql` | `000.init.template.sql` |
| `bin/settings/init_sql/` | `nnn.xxx.sql` | `000.init.sql` |

模板文件的 `nnn` 和 `xxx` 部分与部署文件完全一致，仅多一个 `.template` 后缀。

### 当前文件清单

**模板目录** `bin.template/settings_template/init_sql.template/`：

| 文件 | 说明 |
|------|------|
| `000.init.template.sql` | 建表 DDL，含 `{dimension}` 占位符（运行时替换为实际向量维度） |
| `001.traits_chat_sn_to_chat_id.template.sql` | 数据迁移脚本：将 traits 表从 `chat_sn` 关联改为 `chat_id` 关联 |
| `002.excerpts.template.sql` | 摘录系统建表（excerpt_value_dict、excerpts、excerpt_progress） |
| `003.excerpts_add_msg_time_and_last_ref.sql` | 数据迁移脚本：给 excerpts 表追加 msg_time 和 last_ref_at 列 |
| `unique.init.sql` | 统一建表脚本，语义合自 000~003，供快速初始化使用 |

**部署目录** `bin/settings/init_sql/`：

| 文件 | 说明 |
|------|------|
| `000.init.sql` | 从 `000.init.template.sql` 复制而来 |

## 修改步骤

当需要修改建表语句或新增数据迁移脚本时，按以下步骤操作：

### 1. 编辑模板文件

编辑 `bin.template/settings_template/init_sql.template/` 下的 `.template.sql` 文件。

### 2. 同步到部署目录

将修改后的模板文件复制到 `bin/settings/init_sql/`，**去掉 `.template` 后缀**：

```bash
copy bin.template\settings_template\init_sql.template\nnn.xxx.template.sql bin\settings\init_sql\nnn.xxx.sql
```

> 程序启动时，[`InitSchema`](../../internal/store/pgdb.go:68) 会读取 `bin/settings/init_sql/` 目录下的 `.sql` 文件（跳过以 `-` 开头的文件），并执行其中的 SQL。

### 3. 新增迁移脚本

新增迁移脚本时：

1. 查看 `bin.template/settings_template/init_sql.template/` 目录下已有的 `.template.sql` 文件
2. 取最大 `nnn` 值，新文件序号 = 最大序号 + 1
3. 将迁移脚本写在 `BEGIN;` 和 `COMMIT;` 之间（PostgreSQL 支持 DDL 事务）
4. 同步到 `bin/settings/init_sql/`（去掉 `.template` 后缀）
5. 如需禁用某个 SQL 文件，可在文件名前加 `-` 前缀（如 `-000.init.sql`）

### 4. 同步更新 unique.init.sql（强制）

**每次新增或修改任意 `nnn.xxx.template.sql` 后，必须按 [`unique.init.sql` 合并规范](#uniqueinit-sql-合并规范) 中的原则同步更新 [`unique.init.sql`](../../bin.template/settings_template/init_sql.template/unique.init.sql)。**

这是强制要求，不可跳过。具体操作：

1. 如果新增的表或字段与原脚本中的表无冲突，直接追加到 `unique.init.sql` 对应位置。
2. 如果新增的 `ALTER TABLE` 语义上是对已有表的字段变更，则将该字段直接合并到 `CREATE TABLE` 中（消除迁移逻辑）。
3. 合并后检查字段位置是否语义合理（`create_at`/`update_at` 必须在末尾）。
4. 确保仍满足幂等性（`IF NOT EXISTS`、`ON CONFLICT DO NOTHING` 等）。

### 4. 禁止自动执行的迁移

如果迁移脚本不应在启动时自动执行（如数据迁移只需运行一次），则不放入 `bin/settings/init_sql/`，而是放在 `bin.template/settings_template/init_sql.template/` 作为存档，或单独放在 `bin.template/migrations/` 目录下。

## `unique.init.sql` 合并规范

[`unique.init.sql`](../../bin.template/settings_template/init_sql.template/unique.init.sql) 是 000~NNN 所有 SQL 文件的**语义合并版**，供全量初始化或快速搭建新环境时使用。

### 合并原则

1. **语义合并，非文本拼接。** 不能简单将各文件内容前后拼接，而应将各文件对同一张表的变更合并到一条 `CREATE TABLE` 语句中。
   - 例：002 创建了 `excerpts` 表含 `msg_id` 字段，003 通过 `ALTER TABLE` 追加了 `msg_time` 和 `last_ref_at`。合并时应将 `msg_time`、`last_ref_at` 直接写入 `CREATE TABLE` 的字段列表中，消除 ALTER 步骤。
   - 再例：001 将 `traits` 表从 `chat_sn` 改为 `chat_id`，如果 000 的建表已体现此变更（即 000 中 trait 表已是 `chat_id`），则合并时以此为准，不再保留迁移逻辑。

2. **字段位置语义化。** 合并时，新纳入的字段不应简单追加在末尾，而应根据语义插入到适当位置。以下规则按优先级从高到低排列：

   a. **时间审计字段置末。** `create_at`、`update_at` 始终放在表定义末尾（且 `create_at` 在前，`update_at` 在后）。

   b. **`deleted` 字段位置。** 如果存在 `deleted` 字段，它应位于除 `create_at`/`update_at` 之外的所有字段之后（即紧邻时间审计字段之前）。

   c. **外键紧随主键。** 所有外键字段通常放在主键之后、其他业务字段之前。

   d. **同前缀字段归组。** 拥有相同字段名前缀、且明显描述同一类行为或状态的字段，应彼此相邻归组。例如：
      - `extract_mode`、`extracted_at`、`extracted_count`（提取相关）
      - `title`、`title_state`（标题相关）
      - `msg_id`、`msg_time`（消息相关）

   e. **同前缀短名在前。** 归组时，通常较短的字段名放在前面。例如 `extract_mode` 在前，`extracted_at`、`extracted_count` 在后。

   f. **标志位字段归组。** 优先满足以上规则后，还可尝试将所有「标志/flag」类字段相邻归组（如 `pinned`、`taged`、`deleted`），同时仍满足规则 b 中 `deleted` 的位置要求。

3. **消除迁移逻辑。** 迁移脚本（`ALTER TABLE`、`UPDATE` 回填、`DROP COLUMN` 等）在统一脚本中不再需要，因为这些变更已通过调整建表语句直接达成最终状态。

4. **保留幂等性。** 统一脚本仍需以 `CREATE TABLE IF NOT EXISTS`、`CREATE INDEX IF NOT EXISTS`、`INSERT ... ON CONFLICT DO NOTHING` 等幂等方式书写，确保可重复执行。

### 维护时机

当以下任一情况发生时，应同步更新 `unique.init.sql`：

- 修改了任意 `nnn.xxx.template.sql` 文件中的建表语句
- 新增了含有新表或字段变更的迁移脚本
- 需要为新环境提供一个「开箱即用」的全量初始化脚本

### 与部署文件的关系

| 方面 | `unique.init.sql` | `nnn.xxx.template.sql` |
|------|-------------------|------------------------|
| 定位 | 全量统一脚本 | 增量/迁移脚本 |
| 执行方式 | 手动执行，或开发环境一键初始化 | 由 `InitSchema` 按序号依次执行 |
| 维护者 | 随任意 template SQL 变更同步更新 | 按规范新增/修改 |

`unique.init.sql` **不参与** `InitSchema` 的自动执行，仅作为模板目录中的参考脚本，不复制到 `bin/settings/init_sql/`。

## 注意事项

- 模板中的 `{dimension}` 占位符由 Go 代码在运行时替换为实际向量维度
- 文件名以 `-` 开头的 SQL 文件会被 [`InitSchema`](../../internal/store/pgdb.go:85) 跳过（用于临时禁用）
