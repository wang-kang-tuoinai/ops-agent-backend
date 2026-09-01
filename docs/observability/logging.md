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
Level 0  统计      →  错误率、各级别计数、Top 模板、时间直方图     （0 行原始日志）
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

统一前缀 `/api/v1/observability`，统一 `summary + samples` 双层结构、游标分页、秒级 `start/end` 参数。

### 6.1 `GET /logs/stats` — 统计（Level 0）

请求：`?service=&route=&level=&start=&end=&granularity=60s`

```json
{
  "summary": {
    "window": { "start": 1787000000, "end": 1787003600 },
    "total": 18420,
    "error_count": 392,
    "error_rate": 0.0213,
    "by_level": { "INFO": 17001, "WARN": 1027, "ERROR": 392 },
    "top_templates": [
      { "template": "redis connection refused to {addr}", "count": 8432 }
    ]
  },
  "histogram": [ { "ts": 1787000000, "count": 300, "error": 8 } ],
  "generated_at": 1787003600
}
```
> 已知：一个 5xx 请求会产生两条 ERROR（access log 一条 + 业务错误一条），
> error_rate 的绝对值偏高约一倍。该指标用于观察趋势变化，不代表精确错误率。

### 6.2 `GET /logs/templates` — 模板（Level 1）

请求：`?service=&level=&start=&end=&limit=20`

```json
{
  "items": [
    {
      "template": "redis connection refused to {addr}",
      "count": 8432,
      "first_seen": 1787000100,
      "last_seen": 1787003500,
      "sample": { "ts": 1787003500, "trace_id": "a1b2...", "attrs": { "addr": "redis:6379" } }
    }
  ],
  "has_more": false
}
```

### 6.3 `GET /logs/search` — 过滤原始（Level 2）

请求：`?trace_id=&template=&level=&route=&keyword=&start=&end=&limit=50`

```json
{
  "items": [
    { "ts": 1787003500, "level": "ERROR", "template": "redis connection refused to {addr}",
      "attrs": { "addr": "redis:6379" }, "trace_id": "a1b2..." }
  ],
  "next_cursor": 1787003500,
  "has_more": true
}
```

- `keyword` 走参数绑定，杜绝注入；`limit` 封顶（如 100）。
- 每个样本带 `trace_id`，供 AI 跳 Level 3（[tracing.md](tracing.md) 的 `GET /traces/{trace_id}`）。

## 7. 中间件（自动 access log）

在 router 挂一次 SDK 中间件，**所有路由自动产出 access log**（含 method/route/status/耗时/trace_id），新增接口无需手动埋：

```go
r.Use(sdk.Middleware())   // 一次挂载，全局生效
```

这条日志和 HTTP span 共用同一份埋点，避免重复。中间件的详细设计见 [sdk.md](sdk.md)。
