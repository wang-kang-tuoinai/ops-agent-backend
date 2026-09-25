使用docker compose up --build一键启动🚀

## RabbitMQ 断线恢复

- `Publisher` 和 `Consumer` 各自拥有连接；初始化仍按入口中的启动重试策略处理。
- 运行期间监听连接、Channel 关闭；消费者还监听订阅取消及消息流关闭。恢复间隔为 1、2、4、8、16、30 秒，之后保持 30 秒，直到成功或应用退出。
- 每次恢复重新声明 Exchange；消费者同时重新声明 Queue、Binding 并订阅。每个 `Consumer` 只允许一个订阅，上一代消费循环退出后才会开始下一代。
- 配置冲突、权限等不可恢复的服务端协议错误会停止重试并输出明确日志。修正配置后重新启动对应服务。
- 断线被检测到后，发布直接返回不可用错误，不等待后台恢复。注册接口保留原来的行为：创建成功，发布失败记录 WARN。
- `Close()` 会停止重连并关闭当前连接。消费处理函数应响应传入的 context，以便退出。
- 重连不会自动补发发布失败的消息，也不提供可靠投递保证；当前没有 publisher confirms 或 outbox。未确认的消费可能被重新投递，业务处理仍需考虑幂等。

主要实现：`internal/mq/session.go`（连接及恢复循环）、`publisher.go`（发布）、`consumer.go`（订阅恢复）。连接异常、重试和恢复日志输出到对应进程 stdout。

测试：

```sh
go test ./...
go test -race ./internal/mq
# 需要 Docker；使用独立测试容器，不重启项目中的 RabbitMQ。
go test -tags=integration -run TestRabbitMQRecovery -v -timeout 4m ./internal/mq
```

集成测试覆盖两次 Broker 重启、发布 Channel 关闭、队列删除导致的订阅取消，以及断线期间快速返回错误和退出清理。
