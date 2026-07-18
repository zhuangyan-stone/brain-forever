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

### 4. 禁止自动执行的迁移

如果迁移脚本不应在启动时自动执行（如数据迁移只需运行一次），则不放入 `bin/settings/init_sql/`，而是放在 `bin.template/settings_template/init_sql.template/` 作为存档，或单独放在 `bin.template/migrations/` 目录下。

## 注意事项

- 模板中的 `{dimension}` 占位符由 Go 代码在运行时替换为实际向量维度
- 文件名以 `-` 开头的 SQL 文件会被 [`InitSchema`](../../internal/store/pgdb.go:85) 跳过（用于临时禁用）
