简体中文 · [English](README.en.md)

# go-websocket · WebSocket 定向聊天服务

[![Release](https://img.shields.io/github/v/release/kite88/go-websocket)](https://github.com/kite88/go-websocket/releases/latest)
[![License](https://img.shields.io/github/license/kite88/go-websocket)](LICENSE)

用 Go + Gin 写的一个小服务：浏览器带上 uid 连上来，服务端按消息里的 `receiver`
**精准投递**给某一个用户（不是广播）。页面模板、静态资源、配置模板都用 `//go:embed`
内嵌在二进制里，**clone 下来 `go run .` 就能跑**，运行时不依赖任何外部文件。

定位是**源码实践**：代码分层、并发模型、配置加载与测试都是可以直接读、直接改的规模，
不引入任何构建或运行时的额外依赖。

## 功能

- **定向投递**：按 `receiver` 只发给指定 uid，不广播；接收人不在线会回一条明确的错误提示。
- **身份不可伪造**：`sender` 由服务端按 TCP 连接填写，客户端传什么都不作数。
- **uid 校验**：只允许字母、数字、下划线、连字符，长度 1~64；非法 uid 在**升级握手之前**
  就返回 `400` + JSON 原因，而不是让前端看到一个没有下文的握手失败。
- **并发安全**：连接中心在**单个 goroutine** 里维护 uid → 连接 的映射（map 无锁），
  连接上的写操作全部收敛到每个连接唯一的写循环，不存在并发写同一个 WebSocket 的情况。
- **心跳与超时**：服务端按周期发 ping，等不到 pong（或读超时）就断开，死连接不会堆积。
- **慢客户端保护**：每个连接一个待发送队列，队列满即判定为「对端不读了」并断开，
  慢连接拖不住 Hub，也不会让别人收不到消息。
- **重复 uid 顶号**：同一个 uid 再次连接时替换旧连接，避免消息投到没人看的那条连接上。
- **优雅退出**：`Ctrl+C` / `SIGTERM` 先停止监听，再统一断开全部 WebSocket 后退出。
- **在线列表**：`GET /api/online` 返回当前在线 uid（升序）。
- **前端体验**：亮色 / 跟随系统 / 暗色三态（首帧就应用，暗色不闪白屏）、断线指数退避自动重连、
  点击在线用户即可选中接收人、uid 本地持久化（刷新后身份不变）。
- **地址自适应**：前端按 `location` 拼 `ws://` 或 `wss://`，换端口、换机器、上 https 都不用改代码。

## 目录结构

```text
.
├── main.go                 程序入口：装配配置、路由、优雅退出
├── config/                 配置加载（内嵌 env.ini.<环境> 模板 + 外部 env.ini 覆盖）
├── common/                 通用工具与协议：uid 校验、报文编解码、日志裁剪
├── handle/                 业务实现：连接中心 Hub（hub.go）+ WebSocket 收发（handle.go）
├── router/                 路由装配：页面、静态资源、WebSocket、API 分组
├── test/                   进程内路由测试 + 端到端 WebSocket 测试
├── web/
│   ├── view/index.html     页面模板（内嵌）
│   └── static/             Bootstrap / jQuery / 前端脚本 / 图标（内嵌）
└── go.mod / go.sum         依赖：gin、gorilla/websocket、ini
```

## 快速开始

需要 Go 1.23 及以上。不想装 Go 就直接下二进制：[Releases](https://github.com/kite88/go-websocket/releases/latest)
里每个平台一个单文件可执行程序，页面、静态资源与配置都在里面，下载后直接运行
（Linux / macOS 先 `chmod +x`）。

```bash
# 直接跑（推荐，改完代码重跑即可，不产生文件）
go run .

# 或者编译后运行
go build -o go-websocket . && ./go-websocket
```

启动后会打印版本、配置来源与可访问地址：

```text
2026-10-01 13:43:43 go-websocket dev（release）
配置来源: 内嵌 env.ini.release
本机访问: http://127.0.0.1:8090
局域网访问: http://192.168.1.10:8090
WebSocket 与上述地址同端口，路径 /ws?uid=你的uid
```

开两个浏览器窗口（或一个正常窗口 + 一个隐私窗口），各拿到一个 uid，互相填写对方 uid
就能发消息；也可以直接在左侧「在线用户」里点对方。

### 命令行参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-config` | 空 | 指定外部配置文件（ini）路径；留空时按下面的优先级自动查找 |
| `-version` | `false` | 打印版本号后退出 |

### 配置项

配置文件是 INI 格式（`config/env.ini.<环境>` 是仓库里的模板）：

| 配置项 | 默认值 | 说明 |
| --- | --- | --- |
| `env_mode` | `release` | 运行模式：`debug` / `release` / `test`，取值非法时按 `release` 处理 |
| `server.protocol` | `http` | 仅用于启动日志里拼出访问地址，不改变实际监听方式 |
| `server.http_port` | `8090` | 监听端口 |
| `server.shutdown_timeout` | `10s` | 优雅退出的最长等待时间 |
| `websocket.read_buffer_size` | `1024` | 单个连接的读缓冲区大小（字节） |
| `websocket.write_buffer_size` | `1024` | 单个连接的写缓冲区大小（字节） |
| `websocket.max_message_size` | `4096` | 单条消息的最大字节数，超过会被拒绝 |
| `websocket.ping_period` | `30s` | 服务端发 ping 的间隔；`0` 表示关闭心跳 |
| `websocket.pong_wait` | `60s` | 等待 pong 的超时（同时作为读超时）；`0` 表示不限制 |
| `websocket.write_wait` | `10s` | 单次写超时；`0` 表示不限制 |
| `websocket.send_queue` | `64` | 每个连接的待发送队列长度，队列满即断开 |
| `websocket.allow_all_origins` | `false` | 是否允许任意来源（跨站）连接；默认只允许同源 |

配置来源按优先级从高到低：

1. `-config` 参数 / `GWS_CONFIG` 环境变量指定的文件（指定了就必须存在）；
2. 工作目录下的 `env.ini`；
3. 工作目录下的 `config/env.ini`；
4. 二进制内嵌的 `env.ini.<GWS_ENV>`（`GWS_ENV` 默认 `release`）；
5. 二进制内嵌的 `env.ini.release`。

本地想改配置，四种做法挑顺手的：

- **① 复制模板**：复制成 `config/env.ini`，该文件已被 `.gitignore` 忽略；
- **② `-config` 指定**：直接指向模板文件，仓库里一个文件都不动；
- **③ `GWS_ENV` 选内嵌模板**：取值为 `debug` / `local` / `release` / `test`；
- **④ `GWS_CONFIG` 环境变量**：与 `-config` 等价。

```powershell
Copy-Item config\env.ini.local config\env.ini      # ①
go run . -config config\env.ini.local              # ②
$env:GWS_ENV = 'local'; go run .                   # ③
```

> `web/` 下的页面与静态资源是 `//go:embed` 打进二进制的，**改完必须重新 `go run .`
> 或重启进程**，只刷新浏览器不会生效。

## 接口

| 方法与路径 | 参数 | 说明 |
| --- | --- | --- |
| `GET /` | - | 聊天页面 |
| `GET /ws` | `uid`（必填） | 升级为 WebSocket 连接；uid 非法返回 `400` |
| `GET /api/online` | - | 在线用户列表，`{"count":N,"users":["alice","bob"]}` |

WebSocket 报文（JSON）：

上行（客户端 → 服务端），只有这两个字段有意义，`sender` 由服务端覆盖：

```json
{ "receiver": "bob", "content": "你好" }
```

下行（服务端 → 接收方）：

```json
{ "type": "chat", "sender": "alice", "receiver": "bob", "content": "你好", "time": "2026-10-01 13:43:43" }
```

服务端给**触发者**的提示（接收人离线、报文非法等）：

```json
{ "type": "error", "content": "接收人 nobody 不在线", "time": "2026-10-01 13:43:43" }
```

## 实现要点

- **连接中心单 goroutine 管 map**：`Hub` 的 `clients` 映射只在 `run()` 这一个 goroutine 里
  改动，外部一律通过 channel（`register` / `unregister` / `deliver` / `online`）投递，
  因此 map 不需要加锁。
- **连接只有一个写方**：gorilla/websocket 不支持并发写同一个连接。每个连接有自己的
  `send` 队列，只有 `writePump` 会写连接，其他 goroutine 只投递。
- **不用「关闭 channel」通知退出**：`send` 关闭后任何一次投递都会 panic，而投递方可能来自
  Hub 也可能来自读循环，时序无法保证。改用 `sync.Once` + `close(done)`，投递方永远不会 panic，
  最多是白投一次。
- **慢客户端直接断开**：`send` 队列满说明对端已经不读了，继续堆消息只会连累别人；
  判定后立即断开并回告发送方，这是刻意的取舍。
- **投递失败有回音**：接收人不在线、发送队列已满，都会给发送方回一条 `type=error`。
- **uid 校验前置**：校验放在 `Upgrade` 之前，用 `400` + JSON 说明原因；升级之后再报错
  就只能靠关闭帧，前端拿不到任何信息。
- **sender 服务端覆盖**：`sender` 只信连接身份，客户端传的 `sender` 一律丢弃，避免冒充。
- **读限制**：`SetReadLimit` 限制单条消息大小，否则一个客户端就能用超大帧把内存吃光。
- **心跳走 gorilla 的 ping/pong**：服务端周期发 ping，`SetPongHandler` 里续读截止时间，
  死连接最多 `pong_wait` 就被清理。
- **同源校验**：默认只允许同源连接（页面由本服务托管，同源天然成立）；不带 `Origin` 的
  非浏览器客户端放行，那是调试工具而不是跨站场景。需要放开时配 `allow_all_origins`。
- **`Recovery` 常开**：`gin.New()` 默认不带 Logger / Recovery，handler 里任何 panic 都会
  把进程带崩，这里显式挂上 `Recovery`，并在非 release 模式追加 `Logger`。
- **配置内嵌四份环境模板**：避免「仓库里没有 `env.ini`，clone 下来 `go build` 报
  `no matching files found`」这个老问题；外部 `env.ini` 依然可以覆盖。
- **配置访问带默认值且不会 panic**：`env_mode` 非法时回落 `release`（否则 `gin.SetMode`
  直接 panic）；`GetInt` / `GetBool` / `GetDuration` 缺失或写错都返回默认值。
- **HTTP 超时只限制读请求头**：WebSocket 是长连接，设了 `ReadTimeout` / `WriteTimeout`
  会把正常会话和心跳一起掐断。
- **优雅退出分成两步**：先 `srv.Shutdown` 停止接收新连接，再 `h.Close()` 断开还挂着的
  WebSocket——升级过的连接不在 `http.Server` 的管理范围里，指望 `Shutdown` 收尾会漏掉它们。
- **前端不拼 HTML**：所有用户可控文本都通过 jQuery 的 `.text()` 写进 DOM。uid 与消息内容
  完全由对端控制，拼进 `innerHTML` 就是现成的 XSS。
- **前端地址跟着页面走**：按 `location` 拼 `ws(s)://`，换端口、换机器、上 https 都不用改代码。
- **依赖显式注入**：`handle.New(cfg)` 接收一份参数结构体，测试里可以给「心跳 1 小时」这种
  激进配置，不必改配置文件；`router.R(...)` 接收 `fs.FS`，测试直接用 `os.DirFS("..")`。

## 已知局限

1. **没有鉴权**：任何人只要知道 uid 就能连上来，uid 也不做占用校验（同 uid 直接顶号），
   默认只适合本机或内网使用。
2. **没有持久化**：消息只做转发，离线不补发、历史不落库。
3. **没有 HTTPS / WSS**：`server.protocol` 只影响启动日志里打印的地址，真正的 TLS
   需要由前置 Nginx / Caddy 之类的反向代理终结。
4. **单进程**：连接中心在内存里，多实例部署时不同进程的 uid 互相看不见，
   要横向扩展得引入 Redis 之类的跨进程投递。
5. **没有分组 / 广播**：只支持点对点，没有房间、群聊与离线消息。
6. **在线列表是轮询的**：前端每 5 秒拉一次 `/api/online`，没有做成上行/下行通知。
7. **前端仍是 jQuery + 服务端模板**，没有做前后端分离与构建链路。

## 开发

```bash
go vet ./...    # 静态检查
go test ./...   # 单元测试 + 进程内路由测试 + WebSocket 端到端测试
```

测试覆盖：首页与静态资源、`/api/online`、404、消息定向转发、`sender` 不可伪造、
接收人离线回执、三类非法上行报文、非法 uid 被 400 拒绝、同一 uid 顶号、断开后重连。

> `test/ws_test.go` 里的用例需要真实监听端口（WebSocket 握手要走 Hijack，
> `httptest.NewRecorder` 做不到），用的是 `httptest.NewServer`，不占用固定端口。
>
> 想跑竞态检测需要 cgo（本机要有 gcc）：`CGO_ENABLED=1 go test -race ./...`。

## 发布

发布产物由 `build.sh` 生成：每个平台一个可执行文件，落在 `dist/`，另附 `checksums.txt`
记录全部产物的 SHA256。

```bash
./build.sh              # 版本号取自 git describe --tags
./build.sh -v v1.1.0    # 也可以显式指定
```

脚本需要 bash（Linux / macOS，或 Windows 上的 Git Bash / WSL）。版本号会注入
`main.version`，可用 `go-websocket -version` 核对。

正式发版只需推一个标签，`.github/workflows/release.yml` 会重跑同一套流程
（`go vet` → `go test` → `build.sh` → 校验产物），创建 Release 并上传产物：

```bash
git tag v1.1.0 && git push origin v1.1.0
```

带后缀的标签（如 `v1.1.0-rc1`）发布为 prerelease，不占用 Latest；标签已有 Release 时
改为更新说明并覆盖产物，因此重打标签可以安全地重新发布。想自定义 Release 说明，
把内容写到 `docs/release-notes/<标签>.md` 即可。

## 许可

[MIT](LICENSE) © 2026 kite88
