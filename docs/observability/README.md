# Observability 数据提供方 — 设计文档

> 初代设计（v0.1）。目标：把 `ops-agent-backend` 从「用户 CRUD 演示服务」改造成「可被 AI 查询分析的运维数据提供方」，让 AI 能读日志、读链路、读缓存/布隆命中率等指标。

## 1. 目标

- **给 AI 一个稳定、结构化的运维数据查询面**，而不是让 AI 去看 Jaeger UI 或翻日志文件。
- **新业务接口「自动」可观测、可查询**——新增一个 products 接口，它的日志/链路/指标能被现有工具查到，工具代码零改动。
- **工具可复用、可移植**——将来可包一层 MCP，给多个 agent 复用。

## 2. 核心原则

这几条原则贯穿所有子文档，是所有取舍的准绳。

1. **给 AI 的数据是「Agent 形」，不是「人形」**：默认给聚合摘要，绝不默认给原始流；需要时再下钻抽样本。
2. **阶梯式下钻**：查询从「聚合 → 模板 → 样本 → 链路」逐级深入，token 消耗可控。
3. **工具层只认通用维度，不认业务实体**：过滤条件永远是 `service / operation / route / level / template / trace_id / 时间`，`user`、`product` 只是字符串值。
4. **存储 schema 泛化**：一张通用表 + JSON 列，不为每个业务域建表。
5. **接口全部只读**：AI 默认无破坏性操作，是安全底线。
6. **HTTP-first**：沿用项目「HTTP + JSON + 游标分页」的风格，不过早引入重栈。
7. **可观测性「带外」（out-of-band）**：查询与写入都独立于业务服务（`obs-api`），`ops-agent-backend` 崩溃时照样能查日志/trace 来诊断它。

## 3. 现状盘点与差距

| 数据源 | 现状 | 差距 |
|--------|------|------|
| Trace | OTLP → Jaeger，`handler`/`cache` 已埋 span，`cache.hit` 是 bool 属性 | span 未归一化、未落自建库、无 AI 查询接口 |
| 日志 | `log.Println` 非结构化打 stdout | 无结构化、无采集、无检索接口 |
| 布隆过滤器 | 内存版，只有 `Add`/`MightContain` | 命中/误判/填充率全未度量 |
| Redis 缓存 | 命中与否只写 span 属性 | 无命中率聚合 |
| RabbitMQ | 有 publisher/consumer | 队列深度/lag/ack 未暴露 |

> 关键判断：**布隆命中率/误判率、缓存命中率这些「指标」目前根本不存在**，必须先埋点度量，才能谈暴露。

## 4. 分层架构

```
┌──────────── 诊断 Agent · ops-diagnosis-agent (Python, LangGraph) ────┐
│              LLM 调用 + 工具编排循环（Agent 大脑）                     │
└──────┬────────────────────────────────────────┬──────────────────────┘
       │ 调可观测工具 (HTTP / 将来 MCP)           │ 调 RAG 工具
       ▼                                        ▼
┌──────────────── obs-api (Go) ───────────────┐ ┌─ rag-service (Python) ─┐
│  tools 层：LogStats · LogSearch ·           │ │  RAG 检索/知识          │
│           TraceStats · TraceSearch ·        │ │  （充当 agent 一个工具） │
│           BloomStats · CacheStats ·         │ └─────────────────────────┘
│           QueueStats                        │
│  providers 层：Log / Trace / Bloom /        │
│               Cache / Queue Provider        │
│  ingest：OTLP 接收器 → 归一化 → MySQL       │
└──────┬──────────────────────────────────────┘
       │ 只读查询                  ▲ OTLP 接收
       ▼                          │
  MySQL(logs/spans)  Jaeger  Redis  RabbitMQ
                                    ▲
                                    └─ backend 的 span (OTLP)
```

```
┌──────────── ops-agent-backend（业务服务，只观测不查询）──────────────┐
│  core 层（埋点 SDK）：结构化日志 / span / 指标                        │
│   → 日志：结构化 stdout → 采集进程 → MySQL                           │
│   → span：OTLP → Jaeger + obs-api                                   │
└─────────────────────────────────────────────────────────────────────┘
```

- **诊断 Agent 层**（ops-diagnosis-agent，独立仓库）：LLM 调用 + 工具编排，是本系统唯一「调用大模型」的地方；`rag-service` 降级为它可调用的一个 RAG 工具，与 obs-api 的可观测工具并列。
- **core 层**（在 backend）：语言内部基础原语，让新代码「自动可观测」。
- **providers 层 + tools 层**（在 obs-api）：对接具体系统 + 可观测查询工具。
- **ingest**（在 obs-api）：OTLP 接收器，把 span 归一化落 MySQL，实现「写入带外」。

分层的两层好处：**① 工具与存储解耦**——换日志存储（MySQL → Loki）只换 provider，工具不变；**② 观测与业务解耦**——backend 崩溃不影响查询诊断它。

## 5. 决策记录（ADR）

| 编号 | 决策 | 理由 | 备注 |
|------|------|------|------|
| ADR-001 | 日志/链路分析库用 **MySQL**（个人使用阶段） | 已有 MySQL，贴合「不引入重栈」基调 | 存储层做成 interface，将来可换 Loki/ES |
| ADR-002 | Trace 采用**双写**：Jaeger 给人看（全量原始）+ MySQL 分析库给 AI 查（归一化策展），分工非替代 | Jaeger all-in-one 是内存存储会丢；要自建持久库做 `/traces/stats` 聚合 | 见 [tracing.md](tracing.md) |
| ADR-003 | 工具先走 **HTTP function-calling**，MCP 作为后续可选适配层 | 贴合 HTTP-first；`Tool` 接口与 MCP tool 模型 1:1，包装零成本 | 见 [sdk.md](sdk.md) |
| ADR-004 | 存储 schema **泛化**（通用表 + JSON `attrs` 列） | 新增业务域零 schema 变更，是「新接口自动可查」的前提 | 见 [logging.md](logging.md)、[tracing.md](tracing.md) |
| ADR-005 | 所有 AI 接口**只读**（`GET`） | AI 自动化调用的安全底线 | — |
| ADR-006 | 独立 **obs-api** 服务承担查询（tools+providers）与写入（OTLP 接收归一化落 MySQL）双角色，与 backend 解耦 | 可观测性必须带外：backend 崩了 agent 仍能查询诊断它 | 见 [README.md](README.md) 架构 |
| ADR-007 | 日志/trace 写入由 obs-api/独立进程完成，**不用 backend 进程内直写** | 崩溃瞬间最后几条数据才能保留 | 见 [tracing.md](tracing.md)、[logging.md](logging.md) |

## 6. 文档导航

| 文档 | 内容 |
|------|------|
| [logging.md](logging.md) | 结构化日志模型、模板指纹、阶梯检索、查询接口、泛化表结构 |
| [logging-standard.md](logging-standard.md) | 日志编码规范：4 类日志处置、level 语义、字段命名、哪里记、err 进 attrs 铁律 |
| [tracing.md](tracing.md) | span 归一化、统一词汇映射表、双写存储、查询接口 |
| [sdk.md](sdk.md) | 五样 SDK 原语、「新增 domain 走查」、Tool 接口与 MCP 适配 |

## 7. 落地路线（分四步）

```
第 1 步：core 层原语（backend 内）
        · 结构化日志 (slog) + 中间件（自动 access log + HTTP span）
        · 指标注册表（counter/gauge/histogram）
        · 布隆/缓存计数器（补上「数据不存在」的坑）

第 2 步：obs-api 服务骨架 + 自建分析库
        · 新增 obs-api（Go），跑 OTLP 接收 + 查询 API 两角色
        · 日志表 + span 表（泛化 DDL）
        · span 双写：backend OTLP → Jaeger + obs-api，obs-api 归一化落 MySQL

第 3 步：tools 层（obs-api 内）
        · 六个只读工具 + /observability/tools 自描述接口
        · 统一 summary+samples 响应结构、游标分页

第 4 步（可选）：MCP 适配器
        · 把 Tool 接口包成 MCP server，供多 agent 复用
```
