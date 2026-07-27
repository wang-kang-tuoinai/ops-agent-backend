## 为什么需要异步解耦
消息队列一个非常重要的功能就是异步解耦，比如一个用户注册成功了，你要发邮件提醒用户，那么你不应该把发邮件这就是与注册这个事情写在同一个处理流程里面，因为这会严重拖慢请求的速度，因为发邮件这件事本身比较耗时，如果非要等用户发邮件成功才返回注册成功，用户体验感差。并且解耦有很多好处，比如可以削峰填谷，例如如果对于一个刚发布的APP短时间有很多用户注册，那么对于发邮件的服务来说压力会比较大，因为涉及比较多的网络传输，那么通过引入消息队列，可以让邮件服务以合适的固定的速率去处理，不会导致服务器压力暴增。
## Exchange，Queue，Routingkey，Binding的关系
对于Producer发消息来说，Exchange就像是一个分拣站，Producer往Exchange寄包裹，并且附带上目的地（也就是RoutingKey），这个Exchange连接着许多Queue，同时Exchange内部里面有许多Binding规则，那么当一个"包裹"（也就是生产者发布的事件）到达Exchange的时候，他会用你包裹具体的RoutingKey与Binding规则进行逐一比对，匹配成功，那么就会把这个包裹发到对应的队列，接着继续匹配下面的Binding规则，如果所有的Binding都没有匹配成功，他就会丢弃这个包裹


## Topic模式和Direct模式的区别
Topic模式支持通配符，RabbitMQ里单词都是以.分割的，比如你可以把某个队列的BindingKey声明位#.error那么所有以.error结尾的消息都会被Exchange归为这个队列下面，而*.error就是*匹配一个单词，比如User.error，product.error但是user.register.error是不行的，而Direct不支持通配符是精准的1对1，即一个消息只能被发送到routingKey完全相同的一个队列。值得注意的是，对于Topic类型的Exchange，假如你的Binding里写的全部都是具体的routingKey而没有携带通配符，那么他其实和Direct模式没什么区别，都是一对一。

## Ack，Nack(requeue=false)，Nack(requeue=true)三种消息确认机制
Ack代表消息被正常处理，符合你的业务逻辑，你就可以用Ack代表成功处理了这条消息，这条消息也可以正常从队列里移除。而对于Nack(requeue=false)来说，就代表这条消息本身就有缺陷，你重试再多次也不可能成功（比如消息的格式有问题），此时他就会把他从队列里移除，你可以把这个消息扔到死信交换机里供排查（前提是你配置了死信交换机）。对于Nack(requeue=true)来说就是这条消息处理只是暂时失败了（比如某个下游API超时），但是如果重试可能成功，这时候你可以把消息放进队列里重试。但是要限制重试次数，不能无限制的重试。


## 为什么生产者，消费者要放在两个不同的进程里面
1. 方便独立不是以及独立的生命周期
2. 可以针对性的独立扩容
3. 如果放在同一个进程那么他的容错性不好，消费者有问题会牵连生产者，放在两个进程容错较高。


# 项目里的Rabbitmq

![消息流转图](./images/user_registered_message_flow.svg)
## 消费者处理消息的三条分支
第一条是正常处理成功，那么就直接Ack确认，第二条分支就是应对消息格式有问题的情况，这个时候直接用Nack(requeue=false)，第三种就是下游API有问题的情况，尝试Nack(requeue)重试。


## Rabbitmq里的channel
由于Rabbitmq的channel是并发不安全的，所以项目里采用加锁的形式来避免channel并发冲突的问题，如果维护channel连接池的话，代码复杂度会增加，现阶段，加锁已经够用。

## WaitGroup保证Consumer优雅退出
如果Main函数接收到退出的信，它直接调用Consumer.Close方法直接关闭Channel而不等待Consumer把某个消息彻底消费完，可能会导致消费者消费了某个消息，但是来不及发送Ack，这就让Broker误以为这条消息没有被正确处理，并且此时由于你的消费者被强行退出了，那么Broker会把这条消息再次发送给其他消费者，导致消息被重复处理，但是通过WaitGroup的Wait方法等待，这就能保证消费者是处理完消息自愿退出的，不会说处理到一半突然退出。

## Exchange以及Queue声明的幂等性
重复声明Exchange以及Queue并不会有什么问题，只是如果这个Exchange或者是Queue已经存在了，那么第二次声明会静默失败，通过这个特性，我们可以在生产者和消费者里都声明Exchange，来防止启动顺序造成的影响，比如只在生产者里声明了Exchange，如果消费者先启动，就会导致消费者找不到Exchange而出现问题