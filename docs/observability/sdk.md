# 埋点 SDK 设计

## 1. 目标

> **SDK 的核心不是「定义工具」，而是「新业务代码自动可观测、自动可查询」。**

新增一个 products 接口后，它的日志/链路/指标能被现有工具查到，**工具代码零改动**。注册工具只是 SDK 最表面的一层。

## 2. 两条铁律

### 铁律一：工具层只认通用维度，不认业务实体

工具只按这些通用维度过滤：

```
service / operation / http_route / level / template / trace_id / 时间段
```

`user` 和 `product` 只是过滤条件的字符串值，不是代码：

```http
GET /logs/search?route=/products&level=ERROR
GET /traces/stats?operation=products.create
```

工具代码里没有 `user`/`product` 分支，所以新增 domain 时**工具一行不改**。

### 铁律二：存储 schema 泛化，不按 domain 建表

一张 `logs` 表、一张 `spans` 表，领域专属字段进 JSON `attrs` 列（见 [logging.md](logging.md)、[tracing.md](tracing.md)）。新增 domain = 多写几行，零 schema 变更。

## 3. 五样原语

```
sdk.Middleware()                  ← 挂一次，HTTP access log + HTTP span 全局自动
sdk.Log(ctx)                      ← 结构化日志，自动带 trace_id
sdk.StartSpan(ctx, name)          ← 业务 span
sdk.Counter/Gauge/Histogram(name) ← 指标，自动暴露成可查快照
sdk.RegisterTool(...)             ← 工具注册（最表面的一层）
```

### 3.1 `sdk.Middleware()` — 自动 access log + HTTP span

挂在 router 一次，所有路由自动产出：access log（method/route/status/耗时/trace_id）+ HTTP span。等价于现在的 `otelgin.Middleware` + 结构化日志中间件的合并，避免重复埋点。

### 3.2 `sdk.Log(ctx)` — 结构化日志

```go
sdk.Log(ctx).Error("redis connection refused", "addr", addr, "retry", 3)
```

`trace_id` 从 ctx 自动注入，`template` 由日志库在写入时自动提取，`route` 从 ctx 取。

### 3.3 `sdk.StartSpan(ctx, name)` — 业务 span

```go
ctx, span := sdk.StartSpan(ctx, "products.create")
defer span.End()
span.SetAttributes(sdk.Attr("product_id", id))
```

### 3.4 指标注册表

```go
productsCreated := sdk.Counter("products_created_total")
productsCreated.Inc()
```

- 进程内 registry，统一 `Snapshot()` 输出 JSON，供工具读。
- 可选：同时导出 Prometheus text 格式（不引入重依赖，个人阶段不必）。

### 3.5 `sdk.RegisterTool` — 工具注册

见第 5 节。

## 4. 新增 domain 走查（从 0 加 products 接口）

```go
// router 里加路由（SDK 中间件已在 setup 时挂过一次，这里不动）
api.POST("/products", h.CreateProduct)
api.GET("/products/:id", h.GetProduct)

func (h *ProductHandler) CreateProduct(c *gin.Context) {
    ctx, span := sdk.StartSpan(ctx, "products.create")          // ① 业务 span
    defer span.End()
    span.SetAttributes(sdk.Attr("product_id", id))               // ② 领域属性
    sdk.Log(ctx).Info("product created", "product_id", id)       // ③ 结构化日志
    sdk.Counter("products_created_total").Inc()                  // ④ 指标
    // ...业务逻辑照常写
}
```

分层看，哪些免费、哪些要写一行：

| 能力 | 来源 | 要写几行 |
|------|------|---------|
| HTTP access log（method/route/status/耗时） | `sdk.Middleware`（全局一次） | **0** |
| HTTP span | `sdk.Middleware` 内含 | **0** |
| SQL span（products 表） | GORM 插件（全局） | **0** |
| Redis span | redisotel（全局） | **0** |
| 业务 span（`products.create`） | `sdk.StartSpan` | 1 行 |
| 领域属性（`product_id`） | `SetAttributes` | 1 行（可选） |
| 结构化日志（带 trace_id） | `sdk.Log(ctx)` | 1 行（可选） |
| 指标 | `sdk.Counter` | 1 行（可选） |

**结论：新增 products 后，日志和 trace 自动被 `/logs/*`、`/traces/*` 查到，工具代码零改动；唯一要写的是几行「可选」领域埋点。**

## 5. Tool 接口与 MCP 适配

### 5.1 核心接口

```go
type Tool interface {
    Name()        string          // 工具名
    Description() string          // AI 用「什么时候该调我」来选它
    Schema()      JSONSchema      // 参数 schema
    Execute(ctx, args) (ToolResult, error)
}

sdk.RegisterTool(&BloomStatsTool{})   // 加一个新能力，就这一行
```

SDK 自动：
1. 暴露 `GET /api/v1/observability/tools` → 所有工具 `name + description + schema`（喂给大模型的 function-calling 定义）；
2. 暴露统一调用入口 `POST /api/v1/observability/tools/{name}/call`；
3. （可选）对只读工具自动挂 `GET /api/v1/observability/{name}`。

### 5.2 语言边界

埋点 SDK 是 **Go**（在 backend 里）；AI（rag-service，Python）消费的是**工具定义（JSON Schema，语言无关）**。所以 SDK 是两半：

- **Go 侧**：`metrics` + `slog` + `span` 封装，backend 内部复用；
- **工具注册表**：输出语言无关的 JSON Schema，Python 侧读 `/observability/tools` 即可拿到工具列表。

这一半是真正的可复用点——将来 AI 侧换语言/换模型，工具定义不变。

### 5.3 MCP 适配（ADR-003，可选后路）

`Tool` 接口（`Name/Description/Schema/Execute`）与 MCP tool 模型（`name/description/inputSchema/调用`）**1:1 对应**，包装是机械的、零改动：

```go
func toMCPTool(t Tool) mcp.Tool {
    return mcp.Tool{ Name: t.Name(), Description: t.Description(), InputSchema: t.Schema() }
}
```

- 当前阶段：纯 HTTP（`/observability/tools` + `/tools/{name}/call`），最贴合项目 HTTP-first。
- 将来需复用给多个 agent / 接入 Claude Code、Desktop 等现成 MCP 客户端时：同一套 Tool 包一层 MCP 适配器，半天接上，不推翻前面设计。

> 什么时候直接上 MCP：明确要让**现成 MCP 客户端**用这套能力时，从一开始按 MCP 做，省一层。只服务自己的 rag-service 时，手写 HTTP 更省。
