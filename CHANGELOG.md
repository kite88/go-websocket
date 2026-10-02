# 更新日志

本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)，版本号形如 `v主版本.次版本.修订号`。

## [v1.0.0] - 2026-10-01

首个正式版本：一个可运行、可阅读的定向 WebSocket 消息投递服务。

### 新增

- **定向投递**：按报文里的 `receiver` 只投递给指定 uid，接收人离线时回一条 `type=error` 回执。
- **身份不可伪造**：`sender` 由服务端按连接身份填写，客户端传入的同名字段一律丢弃。
- **uid 校验前置**：只允许字母、数字、下划线、连字符，长度 1~64；非法 uid 在升级握手之前返回 `400` + JSON 原因。
- **WebSocket 通道**：`GET /ws?uid=你的uid`，支持重复 uid 顶号，断开后可重连。
- **在线列表接口**：`GET /api/online` 返回 `{"count":N,"users":[...]}`，uid 升序。
- **聊天页面**：亮色 / 跟随系统 / 暗色三态主题（首帧应用，暗色不闪白屏）、断线指数退避重连、
  点击在线用户选中接收人、uid 本地持久化。
- **命令行参数**：`-config` 指定外部配置文件，`-version` 打印版本号。

### 实现要点

- 连接中心 `Hub` 在单个 goroutine 内维护 uid → 连接 映射，map 无锁，外部一律经 channel 交互。
- 每个连接只有一个写方（`writePump`），其余 goroutine 仅向 `send` 队列投递，避免并发写连接。
- 连接退出用 `sync.Once` + `close(done)` 通知，投递方不会 panic。
- 服务端按周期发 ping，`pong_wait` 超时或读超时即断开死连接。
- 慢客户端（`send` 队列满）直接断开并回告发送方，防止拖垮整个连接中心。
- `SetReadLimit` 限制单条消息大小，避免超大帧耗尽内存。
- 默认只允许同源连接，可用 `allow_all_origins` 放开；非浏览器客户端（无 `Origin`）放行。

### 工程

- 页面模板、静态资源、配置模板全部通过 `//go:embed` 内嵌，`go run .` 即可运行，运行时不依赖外部文件。
- 配置为 INI 格式，内置 `debug` / `local` / `release` / `test` 四份模板，外部 `env.ini` 可覆盖。
- 配置读取带默认值且不 panic：`env_mode` 非法时回落 `release`，`GetInt` / `GetBool` / `GetDuration` 出错返回默认值。
- `Ctrl+C` / `SIGTERM` 优雅退出：先停止监听，再统一断开仍挂着的 WebSocket。
- 版本号由构建时 `-ldflags "-X main.version=..."` 注入，源码直跑显示 `dev`。
- 分层结构：`config` / `common` / `handle` / `router`，依赖通过参数显式注入，便于测试。
- 以 MIT 许可开源，源码与发布产物均可自由使用、修改、分发。
- `build.sh` 一次交叉编译 windows / linux / darwin 六个平台并生成 `checksums.txt`；
  推送 `v*` 标签时 GitHub Actions 自动重跑 `go vet` / `go test` / 构建 / 校验并创建 Release。

### 测试

- `go vet ./...` 静态检查通过。
- `go test ./...` 覆盖首页与静态资源、404、`/api/online`、定向转发、`sender` 不可伪造、
  接收人离线回执、三类非法上行报文、非法 uid 被 400 拒绝、同一 uid 顶号、断开后重连。

### 已知局限

- 无鉴权：知道 uid 即可连接，适合本机 / 内网使用。
- 无持久化：只做转发，不支持离线补发与历史消息。
- 无 TLS：HTTPS / WSS 需由前置反向代理终结。
- 单进程：连接中心在内存中，多实例部署需引入 Redis 等跨进程投递。
- 仅点对点：不支持分组、广播与群聊。

[v1.0.0]: https://github.com/kite88/go-websocket/releases/tag/v1.0.0
