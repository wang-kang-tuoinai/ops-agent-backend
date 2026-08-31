## 1. json:"-"同时被HTTP响应和缓存序列化使用
原因：当时我的本意是想让Redis把加密密码也放在缓存里，但是我错误的复用了HTTP响应的User结构体，导致了Redis没有缓存密码，进而导致从缓存里面查出的不带密码的User结构体覆盖掉了旧的带密码的User结构体，导致更新User的时候，会把密码置空。所以说json:"-"在HTTP响应里是特性，但是在Redis缓存里是Bug
解决办法：为HTTP响应定义单独的UserResponse结构体，这个结构体没有密码属性，直接杜绝了HTTP响应返回密码的可能
教训：为不同的功能创建专门的结构体，比如更新请求，可以做一个更新请求结构体，创建请求，就做一个创建请求的结构体，数据库存储就做一个数据库存储的结构体，响应也做专门的结构体，这样不同功能共用结构体打架的问题就消失了，并且逻辑更清晰
## 2. gorm里的TranslateError没开导致键重复定义的错误没有正确匹配gorm.ErrDuplicatedKey
原因：当时我想给键重复定义加一个自定义的错误类型方便Handler层统一处理，但是当我尝试使用errors.Is(err,gorm.ErrDuplicatedKey)去匹配的时候并没有匹配成功，原来当没有开启TranslateError属性的时候，gorm框架并不会映射Mysql驱动的原始错误，但是开启之后，他就会把这个键重复定义的映射成自己定义的哨兵错误，代码类似fmt.Errorf("%w %w", gorm.ErrDuplicatedKey, originalMySQLError)，这样我们就能成功匹配了。
解决办法：打开gorm的TranslateError错误
教训：ErrRecordNotFound这种gorm自己产生的错误，不用开TranslateErr也可以匹配成功，但是对于ErrDuplicatedKey这种数据库驱动自己报上来的错误，必须开启TranslateErr

## 3.  使用Go里面sync.RWMutex读写锁的时候，没有使用正确的方式解锁
原因：当我使用mu.RLock加了一个读锁的时候，解锁方式用的确实mu.Unlock写锁的解锁方式，这回直接fatal导致程序崩溃
解决办法：使用读锁专门的解锁方式mu.RUnlock
教训：一定要注意自己加的是什么锁，再看这个锁的解锁方式是什么

## 4. 写Docker-compose.yml的时候，以为写了depends on程序就能老老实实按照既定顺序启动
原因:  容器启动完成不代表可以立马连接接收请求，比如Mysql，RabbitMq在容器启动之后可能要等个十几秒或者几十秒才能接收请求。
解决办法：healthCheck，确保能正确连接之后再启动主APP，同时在主App里加上超时重试机制，避免一超时就panic，从而增加容错性。
教训：不要盲目以为容器启动就可以连接，要进行healthCheck检测，同时可以在代码里增加超时重试兜底。

## 5. Redisotel自动记录Redis语句导致密码泄漏
原因：RedisOtel会自动记录你完整执行的Redis语句，如果你的Redis语句里有敏感信息会泄露
解决办法：关掉记录Redis语句，redisotel.WithDBStatement(false)
教训：Redis里不要放密码这种敏感信息，密码最好就保存在数据库里，这样降低了泄密的风险，同时也降低了管理的难度，并且遇到泄漏数据，首先要想这里需不需要它，能删掉的副本比加密副本更加安全。

## 6. gorm框架得Tag少写type前缀，导致静默失效
原因：我在gorm得Tag里把type:varchar(255)写成了varchar(255)就是少写一个type导致他没有报错，并且数据库行为发生了异常，字段绑定不了。
教训：配置类的字段得错误往往是静默的，如果一个字段的行为发生了异常，往往需要和其他正常字段进行逐一比对，看一下哪里有问题。

## 7. goroutine里的log.Fetal导致所有defer没有被执行
原因：log.Fetal会直接退出程序，而不是正常的函数返回，所以defer不会执行
解决办法：让goroutine把错误通过管道发给外层main函数，main函数对错误进行处理，优雅返回
教训：log.Fetal只在没有资源需要清理的时候调用是安全的

## 8. 使用了Docker容器之后，代码里用的还是localhost
原因：当使用容器之后，容器里的localhost不再是宿主机了，所以直接localhost访问会有问题
解决办法：再Docker-compose.yml里配置容器的环境变量，然后代码里优先读取环境变量，如果读到了，说明是用容器启动，那么直接用环境变量，如果没有读取到环境变量，就用本地连接当默认值。
教训：配置走环境变量加默认值，一套代码可以同时适配两种运行环境。

## go-redis 超时参数无法覆盖 DNS 解析失败的场景

现象：Redis 容器 stop 后，无论设置 DialTimeout 还是自定义 Dialer，
错误始终是外层 ctx 的 context deadline exceeded，耗时等于 ctx 超时值。

对照实验（stop vs pause）：
- pause（容器在、进程冻结）→ DNS 可解析 → ReadTimeout 生效 → i/o timeout
- stop（容器没了）→ DNS 解析失败 → 卡在解析阶段 → 走到 ctx 超时

尝试过但未解决：自定义 Dialer + net.Dialer.Timeout。
推测容器环境下 DNS 解析阶段不受 Go context 约束，未进一步验证。

结论：不再依赖错误信息推断服务状态，改用 health 接口直接观测。
教训：能直接观测的状态，不要从错误信息反推。