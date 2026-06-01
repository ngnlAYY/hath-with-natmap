# 参数白名单配置化设计

**目标：** 允许用户通过 YAML 配置文件调整 `natmap` 与 `hath-rust` 的安全白名单参数，同时保持编排器对固定端口、notify、目录、代理和生命周期的控制。

**范围：** 本设计只开放 upstream `natmap` 20260214 与 `hath-rust` v1.17.0 中适合容器编排场景的参数；不提供任意 `extra_args` 透传。

## 背景

当前项目已由 Go 编排器负责生成两个子进程参数：

- `natmap`：固定传入 IPv4 bind 模式参数，包括 `-4`、`-b <network.bind_port>`、`-s <natmap.stun_server>`、`-h <natmap.http_keepalive_server>`、`-k <natmap.keepalive_interval>`、`-e <natmap.notify_script>`。
- `hath-rust`：固定传入目录、端口、代理、日志级别、后台扫描和 RPC server IP 等参数。

用户需要增加配置文件驱动的参数传递能力，但不能破坏本项目的核心约束：`natmap` 和 `hath-rust` 共享固定本地端口，`natmap` notify 必须进入本编排器，Hentai@Home 公网端口更新流程由本编排器控制。

## Upstream 参数调研结论

`natmap` 20260214 的 CLI 参数定义位于 upstream `src/hev-conf.c`，支持：

- 基础参数：`-4`、`-6`、`-u`、`-d`、`-i`、`-k`、`-c`、`-s`、`-h`、`-e`、`-f`。
- bind 参数：`-b`。
- forward 参数：`-C`、`-T`、`-t`、`-p`。

`hath-rust` v1.17.0 的 CLI 参数定义位于 upstream `src/main.rs` 的 `Args`，支持：

- 端口与目录：`--port`、`--cache-dir`、`--data-dir`、`--download-dir`、`--log-dir`、`--temp-dir`。
- 日志和连接：`--disable-logging`、`--flush-log`、`--max-connection`、`-q`。
- 保护与实验功能：`--disable-ip-origin-check`、`--disable-flood-control`、`--enable-metrics`、`--disable-server-header`、`--enable-h3`。
- 代理与现有功能：`--proxy`、`--force-background-scan`、`--rpc-server-ip`。
- 废弃参数：隐藏的 `--sni-strict`。

## 配置结构

新增字段放在现有 `natmap` 和 `hath` 配置节下。

```yaml
natmap:
  address_family: ipv4
  udp_mode: false
  interface: ""
  fwmark: ""
  udp_check_cycle: 10

hath:
  disable_logging: false
  flush_log: false
  max_connection: 0
  disable_ip_origin_check: false
  disable_flood_control: false
  enable_metrics: false
  disable_server_header: false
  enable_h3: false
```

## natmap 参数映射

| 配置字段 | CLI 参数 | 说明 |
|---|---|---|
| `natmap.address_family: ipv4` | `-4` | 默认值，保持现有行为。 |
| `natmap.address_family: ipv6` | `-6` | 允许用户显式使用 IPv6。 |
| `natmap.udp_mode: true` | `-u` | 允许 UDP 模式；默认关闭。 |
| `natmap.interface` | `-i <value>` | 指定网卡名或源 IP。 |
| `natmap.fwmark` | `-f <value>` | 传递 fwmark，支持 upstream 的十进制、八进制和 `0x` 十六进制形式。 |
| `natmap.udp_check_cycle` | `-c <count>` | UDP STUN 检查周期，大于 0 时传递。 |

继续由编排器控制且不开放配置的参数：

- `-b`：必须来自 `network.bind_port`。
- `-s`：继续来自 `natmap.stun_server`。
- `-h`：继续来自 `natmap.http_keepalive_server`。
- `-k`：继续来自 `natmap.keepalive_interval`。
- `-e`：继续来自 `natmap.notify_script`。
- `-d`：不开放，进程生命周期由 supervisor 管理。
- `-t`、`-p`、`-C`、`-T`：不开放，避免切换到 forward mode 或改变连接拓扑。

## hath-rust 参数映射

| 配置字段 | CLI 参数 | 说明 |
|---|---|---|
| `hath.disable_logging` | `--disable-logging` | 禁止写入非错误日志文件。 |
| `hath.flush_log` | `--flush-log` | 每行日志都刷盘。 |
| `hath.max_connection` | `--max-connection <value>` | 仅当值大于 0 时传递。 |
| `hath.disable_ip_origin_check` | `--disable-ip-origin-check` | 同 upstream 语义，会同时影响 server command IP 检查和 flood control。 |
| `hath.disable_flood_control` | `--disable-flood-control` | 禁用 flood control。 |
| `hath.enable_metrics` | `--enable-metrics` | 启用 metrics endpoint。 |
| `hath.disable_server_header` | `--disable-server-header` | 不发送 `Server` header。 |
| `hath.enable_h3` | `--enable-h3` | 启用实验性 HTTP/3。 |

继续由编排器控制且不开放配置的参数：

- `--port`：必须来自 `network.bind_port`。
- `--cache-dir`、`--data-dir`、`--download-dir`、`--log-dir`、`--temp-dir`：继续从 `hath.data_dir` 派生。
- `--proxy`：继续由 `proxy.use_for_hath_downloads` 与 `proxy.url` 决定。
- `--force-background-scan`：保留现有 `hath.force_background_scan` 字段。
- `--rpc-server-ip`：保留现有 `hath.rpc_server_ip` 字段。
- `-q`：继续由 `hath.log_level` 映射。
- `--sni-strict`：已废弃，不开放。

## 校验规则

配置加载阶段必须提前拒绝无效字段值：

- `natmap.address_family` 只能是 `ipv4` 或 `ipv6`。
- `natmap.interface` 可以为空；非空时只能包含网卡名常用字符或合法 IP 字面量，不能包含空白字符或 shell 元字符。
- `natmap.fwmark` 可以为空；非空时只能是十进制、八进制或 `0x`/`0X` 十六进制无符号整数。
- `natmap.udp_check_cycle` 必须大于 0。
- `hath.max_connection` 必须大于等于 0。

所有参数仍通过 `exec.CommandContext` 的 argv 数组传递，不拼接 shell 字符串。

## 数据流

1. `config.Load` 解析并校验新增字段。
2. `cmd/hath-natmap/main.go` 将新增字段复制到 `natmap.RunnerConfig` 与 `hath.Config`。
3. `natmap.RunnerConfig.Args()` 按固定顺序生成 argv：地址族、固定 bind 参数、STUN/HTTP/keepalive/notify 参数，然后追加白名单可选参数。
4. `hath.Config.Args(port)` 在现有固定参数后追加白名单可选参数。
5. supervisor 逻辑不变：仍只依据固定 `BindPort` 启停 `hath-rust`，并拒绝 notify 中 private port 与配置不一致的映射。

## 测试计划

需要覆盖：

- 配置解析：新增字段能正确加载。
- 配置校验：非法 address family、interface、fwmark、udp_check_cycle、max_connection 会失败。
- natmap 参数构造：默认仍生成现有 argv；启用白名单字段时追加对应参数。
- hath-rust 参数构造：默认仍生成现有 argv；启用白名单字段时追加对应参数。
- main wiring：配置中的新增字段会传入 runner/controller。

## 文档计划

更新：

- `configs/config.example.yaml`：加入新增字段和安全默认值。
- `docs/configuration.md`：说明字段含义、默认值、上游参数对应关系和不开放的参数。
- `docs/troubleshooting.md`：补充当 upstream 参数不被支持或配置校验失败时的排查方向。

## 安全约束

- 不支持任意 `extra_args`。
- 不允许用户覆盖固定端口、notify 脚本、hath 数据目录、代理来源或 supervisor 生命周期。
- 不开放 natmap forward mode 参数。
- 不记录敏感配置值或完整 argv 中可能包含敏感信息的代理 URL。
- 所有外部命令参数都以 argv 传递，避免 shell 注入。
