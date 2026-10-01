// Command go-websocket 是一个把「点对点消息投递」做完整的小型 WebSocket 服务：
// 浏览器用 uid 连上来，服务端按消息里的 receiver 精准投递，页面与静态资源
// 全部内嵌在二进制里。
//
// 用法示例：
//
//	go-websocket                  # 使用内嵌的 release 配置
//	go-websocket -config env.ini  # 指定外部配置文件
//	go-websocket -version         # 打印版本号
package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"go-websocket/config"
	"go-websocket/handle"
	"go-websocket/router"
)

// version 由构建脚本用 -ldflags "-X main.version=..." 注入，源码运行时为 dev。
var version = "dev"

var (
	//go:embed web/view/*
	viewFS embed.FS
	//go:embed web/static/*
	staticFS embed.FS
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("程序异常退出: %v", err)
	}
}

func run() error {
	configPath := flag.String("config", "", "外部配置文件路径（ini），也可用 GWS_CONFIG 环境变量指定；留空时依次查找 ./env.ini、./config/env.ini 与内嵌模板")
	showVersion := flag.Bool("version", false, "打印版本号后退出")
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-websocket %s\n", version)
		return nil
	}

	if err := config.Init(*configPath); err != nil {
		return err
	}
	envMode := config.EnvMode()
	gin.SetMode(envMode)

	fmt.Printf("%s go-websocket %s（%s）\n", time.Now().Format(time.DateTime), version, envMode)
	fmt.Println("配置来源:", config.Source())

	// h.Close() 放在 serve 之后：先把监听停掉，再统一断开还挂着的 WebSocket。
	// http.Server.Shutdown 只等普通请求，升级过的连接不在它的管理范围内。
	h := handle.New(webSocketConfig())
	defer h.Close()

	engine, err := router.R(envMode, viewFS, staticFS, h)
	if err != nil {
		return err
	}

	port := config.GetString("server.http_port", "8090")
	printAccessURLs(config.GetString("server.protocol", "http"), port)
	return serve(engine, port)
}

// webSocketConfig 把配置文件里的 WebSocket 参数收集成 handle.Config。
// 集中在一处，方便一眼看出「哪些配置项真的被用到了」。
func webSocketConfig() handle.Config {
	return handle.Config{
		ReadBufferSize:  config.GetInt("websocket.read_buffer_size", 1024),
		WriteBufferSize: config.GetInt("websocket.write_buffer_size", 1024),
		MaxMessageSize:  config.GetInt64("websocket.max_message_size", 4096),
		PingPeriod:      config.GetDuration("websocket.ping_period", 30*time.Second),
		PongWait:        config.GetDuration("websocket.pong_wait", 60*time.Second),
		WriteWait:       config.GetDuration("websocket.write_wait", 10*time.Second),
		SendQueue:       config.GetInt("websocket.send_queue", 64),
		AllowAllOrigins: config.GetBool("websocket.allow_all_origins", false),
	}
}

// serve 启动 HTTP 服务，并在收到 Ctrl+C / SIGTERM 时优雅退出。
func serve(engine *gin.Engine, port string) error {
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: engine,
		// 只限制读请求头：WebSocket 是长连接，设了 ReadTimeout / WriteTimeout
		// 会把正常的心跳和长会话一起掐断。
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP 服务启动失败: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		fmt.Println("收到退出信号，正在关闭服务...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(),
		config.GetDuration("server.shutdown_timeout", 10*time.Second))
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("关闭 HTTP 服务失败: %w", err)
	}
	fmt.Println("服务已停止")
	return nil
}

// printAccessURLs 打印可访问的地址，方便容器 / 局域网部署时直接复制。
func printAccessURLs(protocol, port string) {
	fmt.Printf("本机访问: %s://127.0.0.1:%s\n", protocol, port)

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.Printf("获取本机网络地址失败: %v", err)
		return
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
			continue
		}
		fmt.Printf("局域网访问: %s://%s:%s\n", protocol, ipNet.IP, port)
	}
	fmt.Println("WebSocket 与上述地址同端口，路径 /ws?uid=你的uid")
}
