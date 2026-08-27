# 链路（Trace）数据提供方设计

## 1. 问题

OTel span 里塞满 AI 用不到的东西（`trace_state`、`links`、`events`、`instrumentation_scope`、`schema_url`、几十个 resource 属性），直接喂给 AI 又贵又吵；且不同插桩库字段名不一致（GORM 的 SQL 字段、Redis 的、gin 的都不一样）。所以 **span 必须归一化**。

归一化有两层含义：
1. **裁剪（drop）**：只留 AI 分析真正需要的字段。
2. **统一词汇（mapping）**：把不同来源的同义字段映射到同一个 key。

### 1.1 Jaeger 原始数据长什么样（实证）

`GET /api/traces` 返回的 span 是「多态 tag 数组 + 微秒单位 + references 数组 + processes 表」的结构：

```json
{ "spanID": "c3d4...", "operationName": "cache.GetById",
  "references": [{ "refType": "CHILD_OF", "spanID": "0000..." }],
  "startTime": 1787000000000123, "duration": 4000,
  "tags": [
    { "key": "cache.hit", "type": "bool", "value": true },
    { "key": "internal.span.format", "type": "string", "value": "otlp" },
    { "key": "net.sock.peer.addr", "type": "string", "value": "127.0.0.1:6379" }
  ],
  "logs": [ { "timestamp": 1787000000000200, "fields": [ { "key": "message", "value": "cache miss" } ] } ],
  "processID": "p1" }
```

对照第 2 节归一化后的形态：

| 维度 | Jaeger 原始 | 归一化后 |
|------|------------|---------|
| 属性访问 | 扫描 `{key,type,value}` 数组 | 扁平字段 `span.cache_hit` |
| 单位 | 微秒 | 毫秒 |
| 父子关系 | `references` 数组 | `parent_span_id` 字段 |
| 服务名 | 按 `processID` join | 直接 `service` 字段 |
| 噪音 tag | 全带 | 白名单外丢 `extra`，默认不发给 AI |
| 聚合 | 无，只返回原始 trace | obs-api 提供 `/traces/stats` |

一个 span 原始约 30 行 / ~600 token，归一化后约 8 行 / ~120 token，差 ~5 倍，且 AI 不必自行处理多态数组。

## 2. 规范 span 投影（保留最小集）

```json
{
  "trace_id": "a1b2c3...",
  "span_id": "d4e5f6...",
  "parent_span_id": "00000000",
  "service": "ops-agent-backend",
  "operation": "cache.GetById",
  "start_ms": 1787000000000,
  "duration_ms": 4,
  "status": "error",
  "error": "redis: connection refused",
  "db": "mysql",
  "sql": "SELECT * FROM users WHERE id = ?",
  "cache_hit": true,
  "http_method": "GET",
  "http_route": "/users/:id",
  "user_id": 42
}
```

| 保留 | 说明 |
|------|------|
| `trace_id/span_id/parent_span_id` | 关联与树结构 |
| `service` | 来自 resource |
| `operation` | span 名，语义化（`cache.GetById`、`SELECT users`、`GET /users/:id`） |
| `start_ms/duration_ms` | 时间与耗时 |
| `status/error` | 成败 + 错误消息 |
| 精选属性（下表） | 统一词汇后的少量关键字段 |

| 丢弃 | 说明 |
|------|------|
| `events`、`links` | AI 分析基本用不到 |
| `trace_state`、`instrumentation_scope`、`schema_url` | 纯协议噪音 |
| `dropped_*` 计数 | 无分析价值 |
| 空属性、默认 resource 属性 | 噪音 |

## 3. 统一词汇映射表（归一化核心资产）

一张「规范 key ← 各来源原始 key」的映射表，只保留一个 **~15 个 key 的白名单**，白名单之外丢进 `extra` 字段，**默认不发给 AI**。

| 规范 key | 来源（各插桩库原始字段） |
|----------|--------------------------|
| `db` | `db.system` |
| `sql` | `db.statement`（GORM）/ `sql` |
| `cache_hit` | 自定义 `cache.hit` |
| `cache_key` | Redis key |
| `http_method` | `http.method` |
| `http_route` | `http.route` |
| `status_code` | `http.status_code` |
| `error` | `exception.message` / `error.message` |
| `user_id` | 自定义 `user.id` |
| `product_id` | 自定义 `product.id`（新增 domain 时扩展） |

> 维护方式：新增领域属性时，往这张表和 `extra` 白名单加一行即可，不碰工具代码。

## 4. 两种形态（都要，别二选一）

- **扁平列表**：每个 span 一行，带 `parent_span_id`。→ 用于**聚合查询**（「哪个 operation 最慢、错误率最高」）。
- **树**：完整父子结构。→ 用于**下钻单个 trace**（「这个慢请求卡在哪一层」）。

聚合查询用扁平投影 + 对 `(service, operation, start_ms, status, duration_ms)` 建索引；下钻才还原树。

## 5. 存储：双写，写入带外（ADR-002 / ADR-006 / ADR-007）

```
backend ──OTLP──> Jaeger (4318)             [给人看：全量原始，临时内存]
        └─OTLP──> obs-api (4319) ──归一化──> MySQL   [给 AI 查：策展，持久]
```

- **Jaeger = 全量原始 + 人类瀑布图 UI，临时内存存储**；**MySQL = 归一化策展 + AI 聚合分析，持久存储**。两者分工，不是替代。
- **写入必须带外（ADR-007）**：不由 backend 进程内 `SpanProcessor` 直写 MySQL（backend 崩了最后一个 span 就丢），而由独立 **obs-api** 的 OTLP 接收器承担。backend 配两个 OTLP exporter（OTel Go SDK 原生支持）即可，不碰 Jaeger 现有链路。

> 为什么必须落 MySQL（而不是直查 Jaeger）：Jaeger all-in-one 是内存存储、重启即丢，无法回答「昨天这个接口为什么变慢」；且 `/traces/stats` 的 p50/p95/p99 聚合要对 `(service, operation, start_ms)` 建索引，Jaeger 没有现成接口。落库是持久化 + 自控聚合的前提。

### 5.1 泛化 span 表（ADR-004）

```sql
CREATE TABLE spans (
  trace_id       VARCHAR(64) NOT NULL,
  span_id        VARCHAR(64) NOT NULL,
  parent_span_id VARCHAR(64) NOT NULL DEFAULT '',
  service        VARCHAR(64) NOT NULL,
  operation      VARCHAR(255) NOT NULL,
  start_ms       BIGINT      NOT NULL,
  duration_ms    INT         NOT NULL,
  status         VARCHAR(16) NOT NULL,          -- ok | error
  error          VARCHAR(512) NOT NULL DEFAULT '',
  attrs          JSON        NULL,              -- 归一化后的精选属性
  PRIMARY KEY (trace_id, span_id),
  KEY idx_svc_op_start (service, operation, start_ms),
  KEY idx_start (start_ms)
) ENGINE=InnoDB;
```

- 主键用 `(trace_id, span_id)`，一个 trace 的 spans 物理相邻，查单个 trace 一次范围扫描即得。
- 扁平查询靠 `idx_svc_op_start`；树还原按 `trace_id` 拉全量再内存拼。

## 6. 查询接口（给 AI 的三个工具）

### 6.1 `GET /traces/stats` — 聚合（最值钱）

请求：`?service=&operation=&start=&end=`

```json
{
  "summary": {
    "window": { "start": 1787000000, "end": 1787003600 },
    "total_traces": 1203,
    "error_rate": 0.012,
    "operations": [
      { "operation": "cache.GetById", "count": 8000, "error_rate": 0.005,
        "p50_ms": 2, "p95_ms": 15, "p99_ms": 180 }
    ]
  },
  "generated_at": 1787003600
}
```

直接回答「哪个接口是瓶颈、哪个在报错」。

### 6.2 `GET /traces/search` — 摘要列表（扁平）

请求：`?service=&operation=&error=true&min_duration_ms=&start=&end=&limit=20`

```json
{
  "items": [
    { "trace_id": "a1b2...", "root_operation": "GET /users/:id",
      "duration_ms": 380, "status": "error", "span_count": 5 }
  ],
  "has_more": true
}
```

### 6.3 `GET /traces/{trace_id}` — 归一化树（下钻）

```json
{
  "trace_id": "a1b2...",
  "duration_ms": 380,
  "root": {
    "operation": "GET /users/:id", "service": "ops-agent-backend",
    "duration_ms": 380, "status": "ok",
    "children": [
      { "operation": "cache.GetById", "duration_ms": 4, "cache_hit": true },
      { "operation": "SELECT users", "duration_ms": 210, "sql": "SELECT * FROM users WHERE id = ?" }
    ]
  }
}
```
