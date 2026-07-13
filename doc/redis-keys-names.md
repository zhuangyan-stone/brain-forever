# Redis Key 命名规范

所有 key 统一使用 `d2b.` 前缀以避免多系统共用 Redis 实例时的命名冲突。

## Key 清单

| # | Key 格式 | 类型 | 用途 | TTL | 定义位置 |
|---|---|---|---|---|---|
| 1 | `d2b.S:{sessionID}` | HASH | 用户登录 session（user_id, user_sn, no, nickname, settings 等） | 7 天 | [`sessionKeyPrefix`](../internal/store/cache/session.go:28) |
| 2 | `d2b.smscode:{purpose}:{tel}` | HASH | 短信验证码（code, purpose, sent_at, attempts 等） | 5~10 分钟 | [`smsCodeKeyPrefix`](../internal/store/cache/smscode.go:42) |
| 3 | `d2b.captcha:{action}:{sessionID}` | String (JSON) | 图形验证码 challenge 缓存，按 session 隔离 | 2 分钟 | [`captchaCacheKey`](../internal/user/login.go:34) |
| 4 | `d2b.CAPTCHAS:store:{dir}` | HASH | 预加载的验证码图片与坐标数据（field = 图片名） | 无（持久） | [`provider.go:88`](../infra/captcha/provider.go:88) |
| 5 | `d2b.SESSIONS.GC.scan` | HASH | 内存 session GC 扫描统计（expired_anonymous, online_users 等） | 无（持久, 每次覆盖） | [`gcStatsKey`](../internal/store/cache/session.go:172) |

> **注意**：#2 短信验证码不绑定 session，同一手机号的最新验证码覆盖旧的。这是合理行为——用户手机上最新收到的验证码就是有效的。

## 命名约定

| 模式 | 说明 | 示例 |
|---|---|---|
| `d2b.{小写}:{变量}` | 按 session 或用户隔离的数据 | `d2b.S:{sessionID}`, `d2b.smscode:login:138...` |
| `d2b.{大写}:{变量}` | 全局共享的系统数据 | `d2b.CAPTCHAS:store:d1`, `d2b.SESSIONS.GC.stats` |
