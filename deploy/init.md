# PostgreSQL 初始化指南（Ubuntu Server / Ubuntu）

## 目录

1. [安装 PostgreSQL](#1-安装-postgresql)
2. [初始化数据库集群](#2-初始化数据库集群)
3. [创建用户和数据库](#3-创建用户和数据库)
4. [安装 pgvector 扩展](#4-安装-pgvector-扩展)
5. [配置环境变量](#5-配置环境变量)
6. [验证安装](#6-验证安装)
7. [常见问题](#7-常见问题)

---

## 1. 安装 PostgreSQL

### 1.1 使用 apt 安装

```bash
# 更新包索引
sudo apt update

# 安装 PostgreSQL 16（Ubuntu 24.04 默认源包含 PG 16）
sudo apt install -y postgresql postgresql-contrib

# 验证安装
pg_config --version
# 输出示例: PostgreSQL 16.14 (Ubuntu 16.14-0ubuntu0.24.04.1)
```

> **注意**: 如果系统默认源不包含所需的 PG 版本，可添加 PostgreSQL 官方 APT 仓库：
>
> ```bash
> sudo apt install -y curl gnupg lsb-release
> curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc | sudo gpg --dearmor -o /usr/share/keyrings/postgresql.gpg
> echo "deb [signed-by=/usr/share/keyrings/postgresql.gpg] http://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
> sudo apt update
> sudo apt install -y postgresql-16 postgresql-contrib-16
> ```

### 1.2 检查 PostgreSQL 服务状态

```bash
# 查看服务状态
sudo systemctl status postgresql

# 如果没有运行，启动服务
sudo systemctl start postgresql

# 设置开机自启
sudo systemctl enable postgresql
```

---

## 2. 初始化数据库集群

Ubuntu 上通过 apt 安装 PostgreSQL 后，安装包会自动初始化默认数据库集群（通常位于 `/etc/postgresql/16/main/`），一般**无需手动初始化**。

如需手动初始化（例如在全新系统上重新创建集群），可执行：

```bash
sudo pg_createcluster 16 main --start
```

---

## 3. 创建用户和数据库

### 3.1 切换到 postgres 系统用户

```bash
sudo -i -u postgres
```

以下步骤均在 `postgres` 用户下执行。

### 3.2 创建数据库用户

```bash
# 创建用户（请将 <用户名> 和 <密码> 替换为实际值）
createuser --interactive --pwprompt <用户名>
```

按提示输入：
- 密码：`<密码>`（你设定的密码）
- 是否允许创建数据库：`n`
- 是否允许创建角色：`n`

或者用 SQL 方式创建：

```bash
psql -c "CREATE USER \"<用户名>\" WITH PASSWORD '<密码>';"
```

### 3.3 创建数据库

```bash
# 创建数据库，指定所有者（将 <用户名> 替换为实际值）
createdb --owner=<用户名> d2brain
```

或者用 SQL：

```bash
psql -c "CREATE DATABASE d2brain OWNER \"<用户名>\";"
```

### 3.4 验证用户和数据库

```sql
-- 列出所有用户
\du

-- 列出所有数据库
\l

-- 退出
\q
```

### 3.5 退出 postgres 用户

```bash
exit
```

### 3.6 测试连接

```bash
# 用创建的用户连接数据库（将 <用户名> 替换为实际值）
psql -U <用户名> -d d2brain -h 127.0.0.1

# 系统会提示输入密码
# 连接成功后退出
\q
```

如果连接时报错 `Peer authentication failed`，需要修改 pg_hba.conf，见下方 [常见问题](#71-peer-authentication-failed)。

---

## 4. 安装 pgvector 扩展

### 4.1 通过 apt 安装（推荐）

```bash
# 直接安装
sudo apt install postgresql-16-pgvector
```

如果官方源中没有，添加 pgvector 官方 APT 仓库：

```bash
sudo apt install -y lsb-release
wget --quiet -O - https://dl.pgvector.de/pub/apt/ubuntu/pgvector.gpg.key | sudo gpg --dearmor -o /usr/share/keyrings/pgvector.gpg
echo "deb [signed-by=/usr/share/keyrings/pgvector.gpg] https://dl.pgvector.de/pub/apt/ubuntu $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/pgvector.list
sudo apt update
sudo apt install postgresql-16-pgvector
```

### 4.2 从源码编译安装（备选）

```bash
# 安装编译依赖
sudo apt install -y git build-essential postgresql-server-dev-16

# 克隆并编译 pgvector
git clone --branch v0.7.4 https://github.com/pgvector/pgvector.git
cd pgvector
make
sudo make install
```

### 4.3 在数据库中创建扩展

安装完成后，需要重启 PostgreSQL 使扩展生效：

```bash
sudo systemctl restart postgresql
```

然后连接到目标数据库，创建扩展：

```bash
sudo -u postgres psql -d d2brain -c "CREATE EXTENSION IF NOT EXISTS vector;"
```

验证扩展已安装：

```bash
sudo -u postgres psql -d d2brain -c "SELECT extversion FROM pg_extension WHERE extname = 'vector';"
```

输出应类似：

```
 extversion
------------
 0.7.4
(1 row)
```

---

## 5. 配置环境变量

项目通过环境变量 `PG_DSN` 读取数据库连接信息，格式如下：

```bash
# 格式: postgres://用户名:密码@主机地址:端口/数据库名?sslmode=disable
# 请将 <用户名>、<密码>、<主机地址>、<数据库名> 替换为实际值
export PG_DSN="postgres://<用户名>:<密码>@<主机地址>:5432/<数据库名>?sslmode=disable"
```

### 5.1 写入 bin/.env 文件

项目根目录下的 `bin/.env` 文件用于配置环境变量，格式如下（替换为实际值）：

```bash
PG_DSN=postgres://<用户名>:<密码>@127.0.0.1:5432/d2brain?sslmode=disable
```

### 5.2 连接参数说明

| 参数 | 说明 | 示例值 |
|------|------|--------|
| `host` | 数据库主机地址 | `127.0.0.1` |
| `port` | 数据库端口 | `5432` |
| `user` | 数据库用户名 | `<用户名>` |
| `password` | 数据库密码 | `<密码>` |
| `dbname` | 数据库名 | `d2brain` |
| `sslmode` | SSL 模式（开发环境设为 `disable`） | `disable` |

---

## 6. 验证安装

完成以上步骤后，启动项目即可自动完成数据库初始化：

```bash
# 确保 PostgreSQL 正在运行
sudo systemctl status postgresql

# 确认环境变量已设置
echo $PG_DSN

# 启动项目
go run cmd/server/main.go
```

项目启动时会依次执行：

1. 连接 PostgreSQL（[`internal/store/pgdb.go:24`](../internal/store/pgdb.go:24)）
2. 创建聊天相关表（[`internal/store/chats.go`](../internal/store/chats.go)）
3. 创建 pgvector 扩展（[`internal/store/traits.go:74`](../internal/store/traits.go:74)）
4. 创建 trait 向量相关表（[`internal/store/traits.go:90-105`](../internal/store/traits.go:90:105)）

如果所有步骤正常，不会出现任何数据库相关错误。

---

## 7. 常见问题

### 7.1 Peer authentication failed

**错误信息**:
```
psql: error: connection to server on socket "/var/run/postgresql/.s.PGSQL.5432" failed: FATAL: Peer authentication failed for user "<用户名>"
```

**原因**: PostgreSQL 默认的 `local` 连接使用 `peer` 认证方式，要求操作系统用户与数据库用户一致。

**解决方法**: 修改 pg_hba.conf，将 `local` 行改为 `md5` 密码认证：

```bash
# 查找 pg_hba.conf 位置
sudo find /etc/postgresql -name pg_hba.conf

# 编辑配置文件
sudo vi /etc/postgresql/16/main/pg_hba.conf
```

找到以下行：

```
local   all             all                                     peer
```

修改为：

```
local   all             all                                     md5
```

重启 PostgreSQL：

```bash
sudo systemctl restart postgresql
```

### 7.2 extension "vector" is not available

**错误信息**:
```
ERROR: extension "vector" is not available (SQLSTATE 0A000)
```

**原因**: pgvector 扩展未在操作系统层面安装，PostgreSQL 找不到对应的共享库文件。

**解决方法**: 参考上方 [第 4 节 - 安装 pgvector 扩展](#4-安装-pgvector-扩展)。

### 7.3 数据库端口被占用

```bash
# 检查端口占用
sudo ss -tlnp | grep 5432

# 修改 PostgreSQL 端口（如有冲突）
sudo vi /etc/postgresql/16/main/postgresql.conf
# 修改 port = 5433

# 重启服务
sudo systemctl restart postgresql
```

### 7.4 重置密码

```bash
# 将 <用户名> 和 <新密码> 替换为实际值
sudo -u postgres psql -c "ALTER USER \"<用户名>\" WITH PASSWORD '<新密码>';"
```

### 7.5 删除并重建数据库

```bash
# 将 <用户名> 替换为实际值
sudo -u postgres psql -c "DROP DATABASE IF EXISTS d2brain;"
sudo -u postgres psql -c "CREATE DATABASE d2brain OWNER \"<用户名>\";"
```

---

## 参考链接

- [PostgreSQL 官方文档](https://www.postgresql.org/docs/16/)
- [pgvector GitHub](https://github.com/pgvector/pgvector)
- [项目数据库连接代码](../internal/store/pgdb.go)
- [项目数据库初始化代码](../internal/store/traits.go)
- [数据库配置模板](settings_template/server.template.toml)
- [数据库初始化 SQL 模板](settings_template/init.template.sql)
