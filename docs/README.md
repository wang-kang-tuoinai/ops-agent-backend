# ops-agent-backend学习笔记
暑假Go后端阶段(week1-2)的学习记录，配套代码在本仓库

## 项目演进线索

每一步都是为了解决上一步的问题，而非单纯堆砌技术栈

| 阶段 | 做了什么 | 解决什么问题 |
|---|---|---|
| 1 | 内存 map + Gin CRUD | 跑通基本接口 |
| 2 | [Redis 缓存](redis/redis-in-practice.md) | 重复查询打到存储层 |
| 3 | [布隆过滤器](redis/bloom-filter.md) | 恶意查询不存在的 id,缓存挡不住 |
| 4 | [分布式锁](redis/distributed-lock.md) | 并发更新同一用户相互覆盖 |
| 5 | [RabbitMQ 异步通知](mq/rabbitmq.md) | 注册后的附加操作拖慢主接口 |
| 6 | MySQL 持久化([SQL 优化](database/sql-optimization-checklist.md)) | 重启数据全丢 |
| 7 | Docker Compose 编排 | 五个组件手动启动、顺序易错 |
| 8 | [Jaeger 分布式追踪](tracing/distributed-tracing.md) | 请求慢/出错时不知道卡在哪一层 |


## 按主题查阅

**Redis**
- [三大缓存问题](redis/cache-problems.md) — 会遇到什么坑
- [布隆过滤器](redis/bloom-filter.md)、[分布式锁](redis/distributed-lock.md) — 用什么工具填坑
- [RDB 与 AOF](redis/persistence-rdb-aof.md) — 缓存本身怎么保证不丢
- [项目里的用法](redis/redis-in-practice.md) — 这些东西在真实代码里长什么样

**其他**
- [消息队列](mq/rabbitmq.md)
- [数据库与 SQL 优化](database/sql-optimization-checklist.md)
- [微服务基础概念](microservice/microservice-basics.md)
- [分布式追踪](tracing/distributed-tracing.md)
