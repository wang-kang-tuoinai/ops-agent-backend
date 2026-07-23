- [ ] 给比如数据库连接，Redis，rabbitMQ连接加上重试
- [ ] 给UserName重复定义一个专属的错误
- [ ] 给Span记录具体的SQL以及Redis语句
- [ ] main.go里的if err := srv.ListenAndServe(); err!=nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("HTTP服务启动失败:", err)
		}log.Fetal会导致Defer不能够正常执行