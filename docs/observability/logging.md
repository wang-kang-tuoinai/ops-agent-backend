# 日志数据提供方设计

## 1. 问题

大模型读日志的瓶颈不是「搜不到」，而是 **token 有限 + 海量重复**。1 万行日志里可能 9 千行是同一个模板。因此核心思路是：**在源头把日志折叠成「模板 + 计数」，AI 只读折叠后的东西，需要时才抽少量原始样本。**

当前后端日志全是 `log.Println`（如 `internal/handler/user_handler.go`），裸字符串对 AI 无价值，必须改造为结构化日志。

## 2. 结构化日志模型

每条日志落成一条规范记录（JSON）：

```json
{
  "ts": 1787000000000,
  "level": "ERROR",
  "service": "ops-agent-backend",
  "trace_id": "a1b2c3...",
  "route": "/users/123",
  "template": "redis connection refused to {addr}",
  "attrs": { "addr": "redis:6379", "retry": 3 }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ts` | int64 | 毫秒级 Unix 时间戳 |
| `level` | string | 固定词汇：`DEBUG/INFO/WARN/ERROR` |
| `service` | string | 服务名（固定） |
| `trace_id` | string | 关联链路，自动从 context 注入 |
| `route` | string | 触发日志的 HTTP 路由（`/users`、`/products`） |
| `template` | string | 消息「指纹」，去掉了变量（见下） |
| `attrs` | object | 领域专属变量，统一塞这里 |

## 3. 模板指纹（核心机制）

`template` 是日志的「形状」——把消息里的变量（IP、ID、时间戳、数字、路径参数）替换成占位符，得到消息模板。然后按 `template` 分组聚合，几百万条日志坍缩成几十个模板。

```text
原始:  "redis connection refused to 10.0.0.3:6379"
模板:  "redis connection refused to {addr}"
```

业界常用算法是 **Drain**（树状前缀聚类，在线解析，无需训练）。初代可先做一个简化版：正则替换数字/IP/路径参数/时间戳 → 得模板；复杂场景再引入 Drain3。

**建议写入时就算好 `template`**，查询时直接按模板聚合，快。

## 4. 阶梯式检索（AI 读日志的抽象）

AI 永远**从聚合开始，逐级下钻**，绝不一次性灌原始日志：

```
Level 0  统计      →  各服务 ERROR 日志占比、各级别计数     （0 行原始日志）
Level 1  模板      →  "redis connection refused" 出现 8432 次      （每个模板带 1 条样例）
Level 2  过滤原始  →  按 trace_id/模板/级别/时间窗查，有界返回      （限制 N 行）
Level 3  下钻链路  →  拿 trace_id 跳到 trace 看全貌
```

大多数「最近有没有报错、集中在哪」在 Level 0/1 就解决，根本到不了 Level 2。

## 5. 存储

### 5.1 泛化表结构（ADR-004）

一张通用表，领域专属字段全进 JSON 列，不为每个业务域建表：

```sql
CREATE TABLE logs (
  id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
  ts         BIGINT       NOT NULL,           -- 毫秒时间戳
  service    VARCHAR(64)  NOT NULL,
  level      VARCHAR(16)  NOT NULL,
  route      VARCHAR(255) NOT NULL DEFAULT '',
  template   VARCHAR(255) NOT NULL,           -- 指纹
  attrs      JSON         NULL,               -- 领域变量：product_id/user_id...
  trace_id   VARCHAR(64)  NOT NULL DEFAULT '',
  KEY idx_svc_level_ts (service, level, ts),
  KEY idx_route_ts (route, ts),
  KEY idx_template_ts (template, ts)
) ENGINE=InnoDB;
```

- `user_id`、`product_id` 只是 `attrs` JSON 里的一个键，新增 domain = 多写几行，零 schema 变更。
- 保留策略：个人使用阶段可简单按 `ts` 删旧数据；量级上来后按天分区。

### 5.2 存储选型（ADR-001）

| 方案 | 适合 | 代价 |
|------|------|------|
| **MySQL**（已选） | 个人使用、数据量小 | 自建表 + 索引，最省 |
| Loki | 保留久、多服务、量级上来 | 加服务 + Promtail |
| 进程内 ring buffer | 只看最近 N 条 | 重启即丢 |

**存储层做成 interface**（`LogStore`：`Insert` / `QueryStats` / `QueryTemplates` / `QuerySearch`），将来换 Loki 只换实现，工具不变。

> **写入也带外（ADR-007）**：日志不由 backend 进程内直写 MySQL，而是结构化打到 **stdout**（12-factor 方式），由独立采集进程（可复用 obs-api 的日志采集角色）落 MySQL。这样 backend 崩溃瞬间的最后几条日志仍在 stdout/容器日志缓冲里，不会随进程丢失。

## 6. 查询接口（给 AI 的三个工具）

统一前缀 `/api/v1`，秒级 `start/end` 参数，支持各维度组合过滤。

> **公共参数处理机制与 `notices` 说明**：
> - **时间窗口（`start / end`）**：秒级 Unix 时间戳。非硬性报错校验，但**强烈建议传入以约束扫描范围**。未传时 `end` 默认当前时间，`start` 默认 `end - 3600`（最近 1 小时）；窗口超过 7 天会自动截断为最近 7 天；若 `start >= end` 则重置为 `end - 3600`。
> - **`notices` 提示字段**：三个接口响应顶层均包含 `notices: []string`（若无提示则省略）。当后端对请求参数做了**默认填充、越界修正或截断**（如补充默认时间窗、`limit` 越界重置、`cursor` 格式非法被忽略等）时，会将具体提示写入 `notices`，使调用方（特别是 AI Agent）能够感知实际生效的查询边界。

### 6.1 `GET /logs/stats` — 按服务统计（Level 0）

不传 service 时，按窗口内有匹配日志的服务分别返回统计；指定 service 时只返回该服务。响应顶层显式保留 window，summaries 始终为数组，每项包含 service、total、error_count、error_rate、by_level。无匹配日志返回空数组，不代表所有服务正常。

参数保留 start/end、service、route、method；level 和 top_n 已移除，传入返回 400。不再返回 top_templates。排序为 ERROR 数降序、WARN 数降序、service 升序。

error_rate 是各服务 ERROR 日志占比，不是请求失败率；一次请求可能对应多条 ERROR，不能按固定倍数换算。

完整参数、响应示例和统计口径见 [logs-stats.md](../../../obs-api/docs/logs-stats.md)。

### 6.2 `GET /logs/templates` — 指定服务的模板（Level 1）

service 改为必填；start/end、level、route、method、limit 保持可选。limit 默认 200，范围 1–500。未知服务时先用 stats 发现服务，已知服务可以直接下钻。

响应为 service、items、has_more、notices。按 (template, level) 分组，SQL 绑定 limit+1，多取一组判断是否截断；恰好 limit 组时 has_more 为 false，超过才为 true。返回组的 count 仍统计完整匹配窗口，没有模板分页游标。

sample 仍为该组最新一条，不能推断组内所有错误原因一致。has_more 为 true 时可以增大 limit 或缩小窗口/过滤条件。更多原始日志使用 logs/search，并保留相同 service 筛选。

完整契约见 [logs-templates.md](../../../obs-api/docs/logs-templates.md)。

### 6.3 `GET /logs/search` — 过滤原始日志（Level 2）

通用原始日志检索：按任意维度过滤，返回**真实的原始日志行**（非聚合）。`template` 只是过滤器之一，也可按 `trace_id` / `route` / `keyword` 等组合查询。

#### 6.3 请求参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `start` / `end` | int64 | 否（建议传） | 秒级时间窗，默认最近 1 小时，窗口上限 7 天 |
| `service` | string | 否 | 只查某个服务的日志 |
| `limit` | int | 否 | 每页条数，默认 50，范围 1-100（越界自动重置为 50） |
| `cursor` | string | 否 | 上页返回的 `next_cursor`（`ts:id` 复合值，`ts` 为毫秒），用于翻页；非法格式会被忽略并提示 |
| `trace_id` | string | 否 | 只查某个 trace 的日志 |
| `template` | string | 否 | 只查某个模板的日志 |
| `level` | string | 否 | 只查某个级别（`DEBUG/INFO/WARN/ERROR`） |
| `route` | string | 否 | 只查某个路由（如 `/users`） |
| `method` | string | 否 | 只查某个 HTTP 方法（如 `GET`/`POST`） |
| `keyword` | string | 否 | 模糊子串搜索（`LIKE` 模板与 attrs 文本），须配合时间窗 + limit 避免全表扫 |

```json
{
  "items": [
    {
      "ts": 1787003500,
      "level": "ERROR",
      "service": "ops-agent-backend",
      "route": "/users",
      "method": "POST",
      "template": "redis connection refused to {addr}",
      "attrs": { "addr": "redis:6379" },
      "trace_id": "a1b2..."
    }
  ],
  "next_cursor": "1787003500123:12345",
  "has_more": true,
  "notices": []
}
```

- **分页**：游标分页（keyset）。`next_cursor` 是上页最后一条的 `ts:id` 复合值（`ts` 毫秒级）；下一页传 `?cursor=<next_cursor>`，按 `(ts, id)` 倒序取更旧日志（`ts` 毫秒级会重复，用 `id` 做 tie-breaker）。
- **`keyword`**：对消息内容（模板字符串与 `attrs` JSON 文本）做子串匹配，走参数绑定杜绝注入；因 `LIKE '%x%'` 全表扫，必须配合 `start/end` + `limit`。
- 每条日志带 `trace_id`，AI 据此跳 Level 3（[tracing.md](tracing.md) 的 `GET /traces/{trace_id}`）。

## 7. 中间件（自动 access log）

在 router 挂一次 SDK 中间件，**所有路由自动产出 access log**（含 method/route/status/耗时/trace_id），新增接口无需手动埋：

```go
r.Use(sdk.Middleware())   // 一次挂载，全局生效
```

这条日志和 HTTP span 共用同一份埋点，避免重复。中间件的详细设计见 [sdk.md](sdk.md)。
