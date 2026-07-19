# Go 源代码注释语言规范

## 规则

Go 源代码文件（`.go`）中的注释必须使用 **英文**。

### 适用范围

Go 源代码文件（`.go`）中**所有**注释、说明性文字必须使用英文，无一例外。

包括但不限于：
- 行注释 `// ...`
- 块注释 `/* ... */`
- 函数/方法/类型/常量/变量的 doc comment
- 代码中的任何自然语言说明性文字

### 例外

- `//go:` 指令行
- `//nolint:` 等 linter 指令

### 原因

保持 Go 代码库的注释语言统一为英文，有利于：
- 跨团队协作（非中文母语的开发者也能理解）
- 代码搜索工具（grep、IDE 查找）的统一性
- Go 社区惯例

### 检查方法

```bash
grep -rn '//.*[\x{4e00}-\x{9fff}]' internal/ --include='*.go'
```

---

## 数据库 store 层错误处理规范

### 规则

数据库存储层（`internal/store/` 包内）的错误处理遵循以下原则：

1. **包含完整 SQL 的错误**：如果错误消息中包含了完整的 SQL 语句（例如 SQL 语法错误、约束违例等），**必须**在函数内部使用 `logger.Errorf`（或等效方式）打印 Error 级别日志，然后再将错误返回给调用者。
2. **其他错误**：对于不包含 SQL 语句的普通错误（如 `ErrNotFound`、`ErrDuplicate` 等业务错误），**不得**在 store 函数内打印日志，只需简单地将错误返回给调用者。由调用者根据上下文组织更好的格式并输出日志。

### 原因

- 包含 SQL 的错误在 store 层打印日志可以保留完整的 SQL 上下文，方便排查数据库问题。
- 其他错误由调用者统一包装和记录，避免重复日志、日志格式不一致，以及 store 层过度关注上层日志策略的问题。
- 保持关注点分离：store 层关注数据访问，调用层关注错误处理和日志输出。

### 示例

```go
// ❌ 错误：普通错误在 store 层打印日志
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
    // ...
    if errors.Is(err, sql.ErrNoRows) {
        s.logger.Errorf("user not found: %d", id)  // 不应在此处打日志
        return nil, ErrNotFound
    }
}

// ✅ 正确：SQL 相关错误在 store 层打印日志，普通错误直接返回
func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
    // ...
    if err != nil {
        // 包含完整 SQL 的错误，需要记录日志
        s.logger.Errorf("SQL [%s] args=[id=%d]:\n%v", query, id, err)
        return nil, fmt.Errorf("get user failed. %w", err)
    }
}

// ✅ 正确：普通错误直接返回，由调用者处理日志
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
    // ...
    if errors.Is(err, sql.ErrNoRows) {
        return nil, ErrNotFound  // 直接返回，不在 store 层打日志
    }
}
```
