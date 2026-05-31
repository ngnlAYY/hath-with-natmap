# hath-with-natmap 完全重构设计

## 背景

当前项目 `hath-with-natter` 是一个 Python 包装器，通过内置的 Natter 逻辑获取全锥型 NAT 下的公网映射端口，然后更新 Hentai@Home 设置并启动 `hath-rust`。新版本目标是从零开始重构，不保留旧 Python/Natter 架构和旧配置兼容层，改用 `natmap` 作为公网映射工具。

## 目标

- 使用 Go 编写新的 Docker-only orchestrator。
- 使用现成 `natmap` 与 `hath-rust` 二进制文件，不内嵌或重写二者功能。
- 构建期下载并打包 `natmap` 与 `hath-rust`。
- 使用 natmap bind 模式和固定本地端口。
- 自动更新 Hentai@Home 端口设置并管理 `hath-rust` 生命周期。
- 容器启动时可自动配置 `tc` 上传限速，允许并要求 `NET_ADMIN` capability。
- 提供完善中文文档、中文日志、中文错误信息和必要中文源码注释。
- 代码、Shell 脚本、测试和文档遵循 Google 风格原则。
- 提供覆盖核心逻辑的单元测试与本地模拟集成测试。

## 非目标

- 不兼容旧版 `config.yaml`。
- 不支持裸机 Python 运行模式。
- 不把 `natmap` 或 `hath-rust` 改写为库。
- 不在单元测试中访问真实 e-hentai、STUN 服务或启动真实 `hath-rust`。
- 不默认支持 natmap forward 模式。

## 总体架构

新版本由一个 Go orchestrator 进程作为容器入口。`natmap` 和 `hath-rust` 作为外部子进程运行，orchestrator 负责配置校验、限速配置、进程编排、natmap notify 事件处理、Hentai@Home 设置更新和自动恢复。

建议项目结构：

```text
.
├── cmd/hath-natmap/
│   └── main.go
├── internal/config/
│   ├── config.go
│   └── config_test.go
├── internal/natmap/
│   ├── runner.go
│   ├── notify.go
│   └── notify_test.go
├── internal/hath/
│   ├── client.go
│   └── client_test.go
├── internal/ehentai/
│   ├── settings.go
│   └── settings_test.go
├── internal/bandwidth/
│   ├── limiter.go
│   └── limiter_test.go
├── internal/supervisor/
│   ├── supervisor.go
│   └── supervisor_test.go
├── internal/process/
│   ├── command.go
│   └── command_test.go
├── configs/config.example.yaml
├── scripts/natmap-notify.sh
├── Dockerfile
├── README.md
└── docs/
    ├── configuration.md
    ├── docker.md
    ├── bandwidth-limit.md
    └── troubleshooting.md
```

模块职责：

- `config`：加载、解析和校验全新 YAML 配置。
- `natmap`：构造 natmap bind 模式命令，解析 notify 事件。
- `hath`：构造 hath-rust 命令，写入 `client_login`，管理数据目录。
- `ehentai`：抓取 Hentai@Home 设置页，等待端口可修改，提交新端口。
- `bandwidth`：检查 `tc` 和权限，配置与清理上传限速规则。
- `supervisor`：主状态机，处理映射事件、进程退出、重试和优雅停机。
- `process`：封装子进程启动、停止、信号和输出转发，便于测试替换。

## 配置设计

配置文件使用全新 YAML 格式，容器内默认路径为 `/config/config.yaml`。示例文件放在 `configs/config.example.yaml`。

```yaml
ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"

network:
  bind_port: 4567
  external_update_timeout: 60s

natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh

hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""

proxy:
  enabled: true
  url: http://127.0.0.1:8080
  use_for_hath_downloads: false

bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0

runtime:
  shutdown_timeout: 30s
  restart_delay: 5s
  retry:
    initial_delay: 5s
    max_delay: 5m
```

校验规则：

- `ehentai.member_id`、`pass_hash`、`client_id`、`client_key` 必填。
- `network.bind_port` 必须是合法 TCP 端口。
- duration 字段必须能被 Go `time.ParseDuration` 解析。
- 启动前检查 `natmap.binary_path`、`hath.binary_path` 存在且可执行。
- `hath.data_dir` 必须可创建且可写。
- `proxy.enabled` 为 true 时必须提供合法代理 URL。
- `bandwidth.enabled` 为 true 时必须提供 `upload_limit` 和 `interface`，并检查 `tc` 可用及 `NET_ADMIN` 权限。

配置错误直接导致启动失败，并输出中文错误信息。运行期网络或子进程错误由 supervisor 自动恢复。

## 运行流程

主流程是一个显式状态机：

1. 加载配置并完成启动前校验。
2. 若启用 `bandwidth.enabled`，配置 `tc` 上传限速。
3. 启动 natmap bind 模式：
   - 绑定 `network.bind_port`；
   - 使用配置的 STUN server、HTTP keepalive server 和 keepalive interval；
   - 使用 notify script 把公网映射事件传给 orchestrator。
4. 等待第一条有效 natmap 映射事件。设计假设 notify 事件至少包含公网地址、公网端口、本地端口、协议和本地地址；实现前必须用当前 natmap README 与二进制行为确认参数顺序。
5. 首次收到映射后：
   - 确认 hath-rust 未运行，或先停止 hath-rust；
   - 等待 Hentai@Home 设置页允许修改端口；
   - 提交新的公网端口；
   - 写入 `client_login`；
   - 使用同一个 `bind_port` 启动 hath-rust。
6. 后续相同映射 notify 只刷新内部状态和记录日志，不重启 hath-rust，不重复更新 Hentai@Home。
7. 只有 `public_ip` 或 `public_port` 变化时，才执行“停止 hath-rust → 更新 Hentai@Home → 启动 hath-rust”。
8. natmap 退出时，supervisor 按退避策略重启 natmap。若新映射与旧映射一致且 hath-rust 仍在运行，不做无意义重启。
9. hath-rust 异常退出时，如果当前映射仍有效，按 `runtime.restart_delay` 重启。
10. 收到 SIGTERM 或 SIGINT 时，停止 hath-rust，停止 natmap，尽量清理 `tc` 规则，并在 `runtime.shutdown_timeout` 内退出。

## Hentai@Home 设置更新

`ehentai` 模块使用 `member_id` 和 `pass_hash` 构造 cookie，通过 `client_id` 访问 Hentai@Home 设置页。

更新步骤：

1. GET 设置页。
2. 如果端口字段被禁用，说明客户端仍在线或设置暂不可改，返回可重试错误。
3. 解析现有表单字段和 checked 状态。
4. 保留原有设置，仅替换端口字段。
5. POST 更新后的表单。
6. 对响应做基本失败检查；无法强确认时记录中文警告。

映射变化时必须先停止 hath-rust，再等待并更新 Hentai@Home 端口，最后启动 hath-rust。这样可以避免客户端在线时设置页锁定端口。

## 上传限速

上传限速由 `bandwidth` 模块通过 Linux `tc` 自动配置。该功能是可选配置项，但一旦启用，配置失败就是启动失败，避免用户误以为限速已生效。

运行要求：

- Docker 运行时需要 `--cap-add NET_ADMIN`。
- 容器需要能访问 `tc` 命令。
- 使用 host network 时，`bandwidth.interface` 应配置为实际出口网卡。

行为：

- 启动早期配置 `tc` 规则。
- 规则目标围绕 `network.bind_port` 对应的 hath-rust 上传流量。
- 优雅退出时尽量清理规则。
- 清理失败只记录中文警告，不阻塞容器退出。

## 错误恢复策略

- 配置错误：直接启动失败。
- `tc` 配置失败：直接启动失败。
- natmap 退出：按退避策略重启 natmap，并根据新映射决定是否处理 hath-rust。
- Hentai@Home 更新失败：保持 hath-rust 停止，按退避策略重试更新。
- hath-rust 启动失败或异常退出：当前映射有效时按固定延迟重启。
- 相同映射重复 notify：不重启、不重复提交端口。
- 映射变化：停止 hath-rust，更新端口，再启动 hath-rust。
- 优雅退出：按顺序停止 hath-rust、natmap，并尝试清理 `tc`。

## 代码风格与命名

- Go 代码遵循 Google Go Style Guide、Effective Go、`gofmt` 和 `go vet`。
- Shell 脚本遵循 Google Shell Style Guide，使用 `set -euo pipefail`、明确变量引用和必要函数拆分。
- Go 包名、文件名、变量名和函数名使用英文 Go 惯例。
- 用户文档、配置注释、日志和错误信息使用中文。
- 源码注释使用中文，但只解释非显然原因。
- 导出符号注释遵循 GoDoc 规范。
- 测试优先表驱动风格，测试名称表达行为。

## 测试策略

单元测试覆盖：

- 配置加载与校验。
- natmap notify 参数解析。
- natmap 命令构造。
- hath-rust 命令构造。
- `client_login` 写入。
- Hentai@Home 表单解析、端口字段替换和禁用字段识别。
- HTTP 代理配置传递。
- `tc` 命令构造和失败分类。
- supervisor 状态转移，包括首次映射、相同映射、映射变化、natmap 退出和 hath-rust 退出。

集成测试覆盖：

- 使用 `httptest.Server` 模拟 Hentai@Home 设置页。
- 使用临时目录验证 hath 数据目录和 `client_login`。
- 使用 fake `tc`、fake `natmap` 或 fake process runner 验证 orchestration 行为。

Docker 验证：

- `docker build` 必须成功。
- 文档提供人工验证步骤：检查 `tc` 规则、观察 natmap notify、确认 Hentai@Home 端口更新、确认 hath-rust 启动。

覆盖目标：核心包单元测试尽量达到 80% 以上。真实网络和外部二进制路径以接口测试和文档化手动验证补足。

## 文档计划

- `README.md`：项目用途、Docker-only 说明、快速开始、最小配置、运行命令。
- `docs/configuration.md`：完整配置字段说明和示例。
- `docs/docker.md`：镜像构建、多架构、挂载目录、权限和 host network 注意事项。
- `docs/bandwidth-limit.md`：自动 `tc` 限速原理、`NET_ADMIN`、网卡选择和排障。
- `docs/troubleshooting.md`：Hentai@Home 端口锁定、natmap 无映射、代理失败、hath-rust 启动失败、限速未生效等问题。

文档采用中文，结构遵循 Google developer documentation 风格：先说明用户目标，再给最短可运行路径，最后展开配置、原理和排障。

## 开放风险

- host network 下 `tc` 规则的作用范围依赖实际网卡和规则写法，必须通过文档明确风险，并在实现中尽量限制到 `bind_port`。
- Hentai@Home 设置页是 HTML 表单解析，页面结构变化可能导致更新失败，需要测试覆盖当前解析假设并提供清晰错误。
- natmap release 文件名和平台矩阵需要在实现前确认，Dockerfile 应显式处理不支持的平台。
- `natmap` notify script 的参数格式需要以 natmap README 和实际二进制行为验证。
