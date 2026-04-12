package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"seckill-mall/seckill-service/internal/config"
	"seckill-mall/seckill-service/internal/server"
	"seckill-mall/seckill-service/internal/svc"
	"seckill-mall/seckill-service/seckill"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

var configFile = flag.String("f", "etc/seckill.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	ctx := svc.NewServiceContext(c)

	s := zrpc.MustNewServer(c.RpcServerConf, func(grpcServer *grpc.Server) {
		seckill.RegisterSeckillServiceServer(grpcServer, server.NewSeckillServiceServer(ctx))

		if c.Mode == service.DevMode || c.Mode == service.TestMode {
			reflection.Register(grpcServer)
		}
	})

	// 启动优雅关闭监听（捕获 SIGINT / SIGTERM）
	shutdownDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logx.Info("received shutdown signal, stopping service...")

		ctx.Stop() // 先停止 ServiceContext（关闭 MQ、Redis）
		s.Stop()   // 再停止 gRPC 服务

		// 等待 2 秒让 gRPC 完成清理
		time.Sleep(2 * time.Second)
		logx.Info("shutdown complete, exiting...")
		close(shutdownDone)
		os.Exit(0) // 正常退出
	}()

	fmt.Printf("Starting rpc server at %s...\n", c.ListenOn)
	s.Start() // 阻塞直到 s.Stop() 被调用

	// 等待关闭完成
	<-shutdownDone
}
