# ops-agent-backend

运维 Agent 项目的示例业务后端，使用 Go + Gin 开发。围绕用户 CRUD，接入 MySQL 持久化、Redis 缓存与更新锁、RabbitMQ 注册事件，并用结构化日志和 OpenTelemetry 记录请求行为。

它既是一个可运行的后端服务，也是 Agent 的故障诊断对象：通过 Redis 短暂停顿、MySQL 行锁等待、RabbitMQ 断线等场景，验证系统能否从日志和 Trace 中识别异常、解释影响并找到下一步排查方向。

## 整个项目如何协作

[ops-agent](https://github.com/wang-kang-tuoinai/ops-agent) 由五个模块组成，通过根目录的 Docker Compose 编排：

| 模块 | 职责 |
| --- | --- |
| **`ops-agent-backend`** | 示例业务、注册事件消费者、结构化日志与链路埋点 |
| `obs-api` | 从观测 MySQL 和 Jaeger 查询数据，提供日志/Trace 工具及面板快照 |
| `ops-diagnosis-agent` | 基于 LangGraph 调用观测、知识检索工具，提供会话管理和 SSE 流式回答 |
| `rag-service` | 检索项目架构、排查手册及 Redis/MySQL/RabbitMQ 技术资料 |
| `rag-gateway` | 托管聊天和观测面板，代理诊断与可视化接口 |

典型演示流程：向本服务发送业务流量并注入故障 → 在面板观察日志级别分布、请求耗时和状态 → 同步框选时间窗口 → Agent 查询统计、错误样例及具体调用链，结合知识库给出诊断。

```mermaid
flowchart LR
    Client[业务请求] --> App[ops-agent-backend]
    App --> MySQL[(业务 MySQL)]
    App --> Redis[(Redis 缓存 / 更新锁)]
    App --> MQ[RabbitMQ]
    MQ --> Consumer[注册事件消费者]
    App -->|结构化日志| ObsDB[(obs-mysql)]
    App -->|OTLP HTTP| Jaeger[Jaeger]
    Obs[obs-api] --> ObsDB
    Obs --> Jaeger
    Agent[诊断 Agent / 观测面板] --> Obs
```

当前示例业务是单体服务加独立消费者；跨服务入口统计能力由 `obs-api` 提供。业务库 `ops_agent`、观测库 `observability` 和诊断会话库 `ops_diagnosis` 分别承担不同职责。

## 主要功能与实现

| 能力 | 实现方式 |
| --- | --- |
| 用户管理 | 创建、详情、分页列表、更新、删除；GORM 访问 MySQL；用户名和邮箱唯一约束 |
| 密码处理 | 创建时使用 bcrypt 哈希，响应与缓存中的用户资料不包含密码 |
| 用户缓存 | Redis Cache-Aside；缓存有效期 10 分钟，更新/删除成功后删除缓存 |
| 不存在 ID 过滤 | 进程内布隆过滤器，启动时加载现有用户 ID，创建成功后补充 |
| 更新互斥 | PUT 按用户 ID 使用 Redis SET NX 加锁，UUID 标识持有者，Lua 校验持有者后解锁；锁有效期 4 秒 |
| 注册事件 | 创建成功后向 RabbitMQ 发布事件，独立 consumer 消费并打印注册信息 |
| 日志 | 固定模板、级别与结构化属性写入独立 MySQL，可通过 trace_id 关联链路 |
| Trace | Gin、业务层、缓存层、GORM 和 Redis 埋点，通过 OTLP HTTP 上报 Jaeger |
| 连接恢复 | RabbitMQ 连接/Channel 关闭后的后台重建、消费者重新订阅及退避重试 |

技术栈：Go 1.25、Gin、GORM、go-redis、amqp091-go、OpenTelemetry；根 Compose 使用 MySQL 8、Redis 7 和 RabbitMQ 3 管理版。

### 不同请求的故障表现

| 操作 | 正常路径 | 依赖异常时的当前行为 |
| --- | --- | --- |
| POST 创建 | 密码哈希 → MySQL 写入 → 尝试缓存 → 发布注册事件 | 数据库成功后，缓存写入或事件发布失败会记录异常，仍可返回 200 |
| GET 详情 | 布隆过滤 → Redis；缓存未命中时查 MySQL 并回填 | Redis 读取异常时记录 WARN，回源 MySQL，成功后本次不再回填；缓存内容反序列化失败则返回错误 |
| GET 列表 | 直接查询 MySQL | 不经过 Redis 缓存或更新锁，可用于对比不同接口的故障表现 |
| PUT 更新 | Redis 加锁 → MySQL 更新 → 删除缓存 → 尝试解锁 | 锁竞争返回 409；Redis 加锁异常返回 500，尚未执行更新；数据库成功后的删缓存/解锁失败可能伴随 200 和 WARN |
| DELETE 删除 | 布隆过滤 → MySQL 删除 → 删除缓存 | 数据库成功后的缓存删除失败记录 WARN；此路径不使用 PUT 的更新锁 |

因此，HTTP 200 不能证明缓存同步、事件投递及消费都已完成。分析时需要结合 `cache.hit/store/del`、`handler.lock/unlock/publish` 等 Span 属性与对应日志。

## 快速启动

### 使用根项目 Docker Compose

在 **ops-agent 根目录**执行，首次使用先确保子模块已拉取：

```sh
git submodule update --init --recursive
docker compose up -d --wait mysql obs-mysql redis rabbitmq jaeger
docker compose up -d --build app consumer obs-api
```

`app` 是本服务在 Compose 中的名称。先等待 `obs-mysql` 就绪，是因为后端启动时会迁移日志表，而当前 `app` 的 Compose 依赖列表没有单独等待它。

| 地址 | 用途 |
| --- | --- |
| `http://localhost:8080/api/v1/users` | 用户业务接口 |
| `http://localhost:8082` | obs-api，查询本服务产生的日志和 Trace |
| `http://localhost:16686` | Jaeger UI，选择服务 `ops-agent-backend` |
| `http://localhost:15672` | RabbitMQ 管理页面，本地 Compose 默认 guest/guest |

启动时自动迁移业务 `users` 表，以及观测库 `logs` 表及其索引。业务数据库和观测数据库本身由 Compose 的 MySQL 初始化配置创建。只运行上述服务不需要模型密钥；完整对话演示需额外配置并启动 Agent、RAG 和网关。

### 本地开发

需要 Go 1.25+，以及可访问的业务 MySQL、观测 MySQL、Redis、RabbitMQ 和 Jaeger。在本仓库目录执行，以下为 PowerShell 示例：

```powershell
$env:MYSQL_DSN = "root:root@tcp(127.0.0.1:3306)/ops_agent?charset=utf8mb4&parseTime=True&loc=Local"
$env:OBS_MYSQL_DSN = "root:root@tcp(127.0.0.1:3307)/observability?charset=utf8mb4&parseTime=True&loc=Local"
$env:REDIS_ADDR = "127.0.0.1:6379"
$env:RABBITMQ_ADDR = "amqp://guest:guest@127.0.0.1:5672/"
$env:OTEL_EXPORTER_OTLP_ENDPOINT = "http://127.0.0.1:4318"
go run .
```

另开终端，在同一仓库目录启动消费者：

```powershell
$env:RABBITMQ_ADDR = "amqp://guest:guest@127.0.0.1:5672/"
go run ./cmd/consumer
```

本地服务固定监听 `:8080`，不要与容器版 `app` 同时占用该端口。consumer 是后台消费进程，没有 HTTP 接口。程序读取进程环境变量，不自动加载 `.env`。

## 环境变量

| 变量 | 本地默认值或示例 | 说明 |
| --- | --- | --- |
| `MYSQL_DSN` | `root:root@tcp(127.0.0.1:3306)/ops_agent?charset=utf8mb4&parseTime=True&loc=Local` | 业务数据库；Compose 使用 `mysql:3306` |
| `OBS_MYSQL_DSN` | `root:root@tcp(127.0.0.1:3307)/observability?charset=utf8mb4&parseTime=True&loc=Local` | 日志数据库；Compose 使用 `obs-mysql:3306` |
| `REDIS_ADDR` | `localhost:6379` | 用户缓存与更新锁；Compose 使用 `redis:6379` |
| `RABBITMQ_ADDR` | `amqp://guest:guest@localhost:5672/` | 发布者和消费者分别读取；Compose 使用主机名 `rabbitmq` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 显式设置为 `http://127.0.0.1:4318` | OTLP HTTP 上报地址；Compose 使用 `http://jaeger:4318` |

表中的 root/root、guest/guest 是现有本地演示配置。日志与 Trace 的服务名目前固定为 `ops-agent-backend`，并非通过环境变量配置。

## 业务 API

路由前缀为 `/api/v1`。成功时直接返回用户对象或数组；失败时返回 `{"error":"说明"}`，没有统一外层 data 包装。

| 方法 | 路径 | 请求与响应 |
| --- | --- | --- |
| POST | `/users` | 必填 username、email、password，可选 age；成功 200，返回用户 |
| GET | `/users/:id` | 成功 200，返回用户；不存在为 404 |
| GET | `/users` | 可选 page、limit；默认第 1 页、10 条，最多 100 条；返回数组 |
| PUT | `/users/:id` | 可选 username、email、age，仅更新传入字段；成功 200 |
| DELETE | `/users/:id` | 成功 200，无响应正文 |

参数错误通常返回 400，用户名/邮箱冲突和更新锁竞争返回 409，未预期的内部错误返回 500。

PowerShell 示例（会创建一条演示用户记录）：

```powershell
$suffix = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$body = @{
    username = "demo_$suffix"
    email = "demo_$suffix@example.com"
    password = "demo-password"
    age = 21
} | ConvertTo-Json
$user = Invoke-RestMethod -Method Post -Uri "http://localhost:8080/api/v1/users" -ContentType "application/json" -Body $body
Invoke-RestMethod "http://localhost:8080/api/v1/users/$($user.id)"
Invoke-RestMethod "http://localhost:8080/api/v1/users?page=1&limit=10"
```

响应包含 id、username、email，以及非零时的 age，不返回密码。请求完成后可在 Jaeger 与 obs-api 查看对应观测数据。

## 观测数据

`internal/observability` 将 `ts`（Unix 毫秒）、service、level、route、method、template、attrs 和 trace_id 写入 `observability.logs`。访问日志记录状态码、耗时与实际路径；业务日志记录缓存失败、锁竞争、数据库错误及消息发布失败等事件。

route 使用 Gin 路由模板，例如 `/api/v1/users/:id`；未匹配路径记录为 `<unmatched>`。当前访问日志对 5xx 记 ERROR，其他状态记 INFO；业务日志另外表达预期冲突或异常，不能仅按 HTTP 状态推断日志级别。

Trace 保留入口、业务操作及数据库/缓存调用关系。`SetStatus` 的描述和 `RecordError` 的异常信息分别保留，已处理的重复键冲突通过业务属性标记，供 obs-api 分类时识别。GORM 埋点关闭 SQL 参数记录，Redis 埋点关闭命令正文记录。

日志目前同步写入观测库，写入失败输出到进程日志；没有持久化补偿队列。消费进程及 RabbitMQ 连接恢复日志输出到 stdout，尚未统一进入结构化日志表，也未通过 AMQP headers 贯通消费者 Trace。

## RabbitMQ 断线恢复

- `Publisher` 和 `Consumer` 各自拥有连接；初始化仍按入口中的启动重试策略处理。
- 运行期间监听连接、Channel 关闭；消费者还监听订阅取消及消息流关闭。恢复间隔为 1、2、4、8、16、30 秒，之后保持 30 秒，直到成功或应用退出。
- 每次恢复重新声明 Exchange；消费者同时重新声明 Queue、Binding 并订阅。每个 `Consumer` 只允许一个订阅，上一代消费循环退出后才会开始下一代。
- 配置冲突、权限等不可恢复的服务端协议错误会停止重试并输出明确日志。修正配置后重新启动对应服务。
- 断线被检测到后，发布直接返回不可用错误，不等待后台恢复。注册接口保留原来的行为：创建成功，发布失败记录 WARN。
- `Close()` 会停止重连并关闭当前连接。消费处理函数应响应传入的 context，以便退出。
- 重连不会自动补发发布失败的消息，也不提供可靠投递保证；当前没有 publisher confirms 或 outbox。未确认的消费可能被重新投递，业务处理仍需考虑幂等。

主要实现：`internal/mq/session.go`（连接及恢复循环）、`publisher.go`（发布）、`consumer.go`（订阅恢复）。连接异常、重试和恢复日志输出到对应进程 stdout。

## 目录结构

```text
main.go                   HTTP 服务启动、依赖初始化、表迁移与 Trace 配置
cmd/consumer/main.go      注册事件消费者入口
internal/
  router/                 路由注册
  handler/                用户接口、错误到 HTTP 响应的映射
  model/                  用户模型与请求/响应结构
  repository/             存储接口、MySQL 实现与内存实现
  cache/                  Redis 缓存装饰层
  bloom/                  进程内布隆过滤器
  utils/                  Redis 更新锁
  scripts/                解锁 Lua 脚本及嵌入资源
  mq/                     消息发布、消费、连接恢复与测试
  observability/          结构化日志、访问日志中间件与数据模型
  apperr/                 组件错误分类
docs/                     学习笔记、设计说明与观测文档
Dockerfile                HTTP 服务镜像
Dockerfile.consumer       消费者镜像
```

## 测试与故障演练

在本仓库目录运行单元测试；目前自动化测试主要集中于 RabbitMQ 连接恢复逻辑，尚未覆盖全部业务接口：

```sh
go test ./...
go test -race ./internal/mq
```

RabbitMQ 集成测试需显式启用：

```sh
# 需要 Docker；使用独立测试容器，不重启项目中的 RabbitMQ。
go test -tags=integration -run TestRabbitMQRecovery -v -timeout 4m ./internal/mq
```

集成测试覆盖两次 Broker 重启、发布 Channel 关闭、队列删除导致的订阅取消，以及断线期间快速返回错误和退出清理。

根项目另提供 [故障演练脚本说明](https://github.com/wang-kang-tuoinai/ops-agent/blob/main/fault-testing.md)。在自己的本地测试环境、ops-agent 根目录执行：

```powershell
python -m pip install -r requirements-test.txt
python test.py --dry-run
python test.py
```

默认演练持续 30 分钟，基础调度 3 QPS，创建专用测试用户，并穿插 Redis 暂停、MySQL 局部行锁等待、RabbitMQ 断线和异常请求模式。正式运行会修改测试数据及容器状态，应先阅读说明并检查 dry-run；结果时间线用于核对 Agent 诊断，不代表自动计算出的诊断准确率。

## 当前边界

- 示例业务尚未实现登录鉴权与权限管理，定位为后端学习和故障诊断演示。
- 缓存删除失败可能残留旧数据，目前没有补偿机制；Redis 锁没有自动续租或 fencing token，不能宣称提供严格的分布式一致性。
- 布隆过滤器是单进程状态，启动加载、应用内创建后更新，不能自动感知外部直接写库或其他实例新增的数据。
- 注册事件消费者目前只打印信息，未实现真实邮件发送；RabbitMQ 自动重连不等于可靠投递，也不保证消息只消费一次。
- 默认 Compose 的 Jaeger 未配置持久化存储；观测数据与业务正确性、完整采集需要分别验证。

## 延伸阅读

- [学习笔记与项目演进](docs/README.md)
- [可观测性文档](docs/observability/README.md)
- [分布式追踪实践](docs/tracing/distributed-tracing.md)
- [Redis 缓存实践](docs/redis/redis-in-practice.md)
- [Redis 分布式锁](docs/redis/distributed-lock.md)
- [RabbitMQ 笔记](docs/mq/rabbitmq.md)

这些文档包含项目演进过程；当前运行方式与行为以本 README 及代码为准。
