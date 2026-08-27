# 日志编码规范

> 本文档定义**开发者怎么写日志**的标准：哪些日志要记、记在哪、什么格式、level 怎么定。与 [logging.md](logging.md)（讲"日志数据怎么存、怎么查"）配合阅读。使用方式见 [sdk.md](sdk.md) 的 `sdk.Log(ctx)`。

## 1. 目标

- 让日志**结构化、可聚合、可被 AI 查询**。
- 让新代码**有章可循**，不再出现 `log.Println` 满天飞、同一错误记三遍的情况。
- 把「有价值日志」和「依赖噪音」分开，只让前者进入 AI 查询面。

## 2. 日志分类（4 类 + 处置）

不是所有日志都平等。这是最上层的一条分界。

| 分类 | 来源 | 示例 | 处置 |
|------|------|------|------|
| **访问日志** | 中间件自动，每请求 1 条 | `POST /users 200 313ms` | 结构化，进 store，`INFO` |
| **业务/事件日志** | handler/service 显式写 | `"user created"`、`"publish event failed"` | 结构化，进 store |
| **错误日志** | 错误处理点 | `"internal error"` + err | 结构化，进 store，`ERROR` |
| **依赖/库日志** | MySQL/Redis/RabbitMQ 驱动内部 | `closing bad idle connection`、`write: broken pipe` | **抑制或降 `DEBUG`，不进 store** |

依赖日志举例（**不要记**，是正常现象不是错误）：

```text
[mysql] closing bad idle connection: unexpected read from socket
[mysql] write tcp ...: broken pipe
```

## 3. level 语义

| level | 什么时候用 |
|-------|-----------|
| `DEBUG` | 依赖库噪音、调试细节（不进 store，或短保留） |
| `INFO` | 访问日志、正常业务事件、防御性拦截（布隆过滤） |
| `WARN` | 可降级错误：缓存失败回源、重试、锁获取失败、事件发布失败 |
| `ERROR` | 真正的错误：请求失败、未处理异常、最终落库失败 |

判断口诀：**"这个错误影响本次请求成功了吗？"** 不影响（降级兜住了）→ `WARN`；影响了 → `ERROR`。

## 4. 固定字段命名（key 统一，全项目一致）

| key | 含义 | 备注 |
|-----|------|------|
| `error` | 错误对象 / 错误消息 | 见第 6 节铁律 |
| `trace_id` | 链路 ID | **自动注入，不要手写** |
| `route` | HTTP 路由 | 中间件自动注入 |
| `user_id` / `product_id` | 业务实体 ID | 新增 domain 时扩展 |
| `cost_ms` | 耗时 | |
| `queue` | 队列名 | |
| `key` | 缓存 / 锁的 key | |

## 5. "哪里记录"三条原则

1. **访问日志只记一次、由中间件自动记**，业务代码永远不要手写"请求来了/返回了"。
2. **错误只在一个点记**——错误最终被处理/上报的地方（如 `HandleError`）。不要在 repository → cache → handler 每层都 `log.Println(err)`，否则同一错误记三遍。
3. **业务事件只在"发生了有意义的事"时记**：创建成功、消息发布失败、锁获取失败等。正常走完的流程不必记。

## 6. 核心铁律：消息静态，变量进 `attrs`

> **消息字符串必须静态，所有变量（含 `err`）走 key-value 进 `attrs`。禁止 `Printf("...%v", x)` 把变量拼进消息。**

这是本规范最重要的一条，直接决定日志能否被正确聚合：

```go
// ❌ 变量拼进消息 → 每个不同错误都是一个独立模板，聚合碎成渣
log.Println("internal error: ", err)
log.Printf("用户注册事件发布失败:userId=%d err=%v\n", id, err)

// ✅ 消息静态，变量进 attrs
sdk.Log(ctx).Error("internal error", "error", err)
sdk.Log(ctx).Warn("publish user register event failed", "user_id", id, "error", err)
```

为什么：模板指纹是从**消息字符串**提取的（见 [logging.md](logging.md) 第 3 节）。消息静态 → 所有同源错误聚合到同一个 `template`；变量拼进消息 → 得到几十个碎片模板，折叠失效。

正确落库结果：

```json
{
  "template": "internal error",
  "attrs": { "error": "duplicate entry for key 'users.username'" }
}
```

## 7. 现有代码 → 规范写法对照

| 现有写法 | 问题 | 规范写法 |
|----------|------|---------|
| `log.Println("internal error: ", err)` | err 拼进消息 | `sdk.Log(ctx).Error("internal error", "error", err)` |
| `log.Printf("用户注册事件发布失败:userId=%d err=%v\n", id, err)` | 变量拼进消息、无 level | `sdk.Log(ctx).Warn("publish user register event failed", "user_id", id, "error", err)` |
| `log.Println("blocked by bloom filter:", strID)` | 变量拼进消息 | `sdk.Log(ctx).Info("blocked by bloom filter", "user_id", strID)` |
| `log.Println("Cache user failed:", err)` | 无 level、err 拼进消息 | `sdk.Log(ctx).Warn("cache user failed", "user_id", id, "error", err)` |

## 8. 具体落地项

1. **换掉 `gin.Default()`**：它内置文本格式 Logger（就是 `[GIN]` 那行）。改为 `gin.New()` + 结构化访问日志中间件（`sdk.Middleware()`）+ `gin.Recovery()`。
2. **全局替换 `log.Println` / `log.Printf`** 为 `sdk.Log(ctx).*`，`ctx` 从 handler/service 的 context 传入。
3. **抑制依赖库日志**：把 MySQL/Redis 驱动内部日志路由到 `DEBUG` 或 no-op logger，不进入 AI 查询面。
4. **错误单点化**：梳理 repository/cache 层的降级 `log`，确认每处 level 正确（降级 → `WARN`），并保留结构化字段。
