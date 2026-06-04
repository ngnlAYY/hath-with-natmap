# 配置说明

配置文件默认路径是 `/config/config.yaml`。容器启动时会先校验配置，配置错误会直接终止启动。

可以从示例文件开始：

```bash
cp configs/config.example.yaml config.yaml
```

## mapping

```yaml
mapping:
  mode: natmap
```

- `mode`：端口映射模式，只能是 `natmap` 或 `upnp`；默认 `natmap`。
- `natmap`：使用现有的 STUN / NAT 映射流程获取公网端口。
- `upnp`：使用路由器 UPnP/IGD 添加 TCP 端口映射；公网端口和本地端口都等于 `network.bind_port`。
- `upnp` 失败不会自动回退到 `natmap`，需要根据日志和网络环境单独排查。

## ehentai

```yaml
ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"
  skip_port_update: false
```

- `member_id`：e-hentai Cookie `ipb_member_id`。
- `pass_hash`：e-hentai Cookie `ipb_pass_hash`。
- `client_id`：Hentai@Home client ID。
- `client_key`：Hentai@Home client key。
- `skip_port_update`：是否跳过访问 E-Hentai 更新 Hentai@Home 公网端口；默认 `false`。设为 `true` 时仍会执行 `natmap` 或 UPnP 映射并启动 `hath-rust`，但不会访问 Hentai@Home 设置页修改端口，此时 `member_id` 和 `pass_hash` 可以留空。

这些值用于登录 Hentai@Home 设置页和生成 `hath-rust` 的 `client_login` 文件。不要把真实配置提交到仓库。

## network

```yaml
network:
  bind_port: 4567
  external_update_timeout: 60s
```

- `bind_port`：`natmap` 和 `hath-rust` 共享的固定本地端口。
- `external_update_timeout`：更新 Hentai@Home 端口时的 HTTP 超时时间。

## natmap

```yaml
natmap:
  binary_path: /usr/local/bin/natmap
  stun_server: stun.nextcloud.com:3478
  http_keepalive_server: www.baidu.com:80
  keepalive_interval: 15s
  notify_script: /usr/local/bin/natmap-notify.sh
  address_family: ipv4
  udp_mode: false
  interface: ""
  fwmark: ""
  udp_check_cycle: 0
```

默认镜像已经包含 `natmap` 和 notify 脚本，通常不需要修改 `binary_path` 与 `notify_script`。

- `stun_server`：用于探测 NAT 映射的 STUN 服务，对应 `natmap -s`。
- `http_keepalive_server`：用于 keepalive 的 TCP 目标，对应 `natmap -h`。
- `keepalive_interval`：natmap keepalive 间隔，对应 `natmap -k`；必须至少为 `1s` 且为整秒。
- `address_family`：地址族，只能是 `ipv4` 或 `ipv6`；默认 `ipv4`，分别对应 `-4` 或 `-6`。
- `udp_mode`：是否启用 UDP 模式；默认 `false`，为 `true` 时传递 `-u`。
- `interface`：可选网卡名或源 IP；默认空，不传递；非空时对应 `-i`。
- `fwmark`：可选 fwmark，支持十进制、八进制或 `0x` 十六进制无符号整数；默认空，不传递；非空时对应 `-f`。
- `udp_check_cycle`：UDP STUN 检查周期；默认 `0`，表示不传递 `-c` 并沿用 upstream 默认；大于 `0` 时对应 `-c`。

`natmap -b` 固定来自 `network.bind_port`，`-e` 固定来自 notify 脚本，`-s/-h/-k` 继续来自上面的结构化字段。编排器不会开放 `-d`、forward mode 参数 `-C/-T/-t/-p`，也不支持 raw `extra_args`。

## upnp

```yaml
upnp:
  lease_duration: 0
  description: hath-with-natter
```

- `lease_duration`：UPnP 映射租期，不能小于 `0`；`0` 表示永久映射或由路由器使用默认行为。程序正常退出时仍会主动删除映射。
- `description`：路由器后台显示的端口映射描述；不能为空。
- UPnP 模式只映射 TCP，不映射 UDP。
- 适用于路由器支持 UPnP/IGD，且允许局域网客户端主动添加端口映射的网络环境。

## hath

```yaml
hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
  disable_logging: false
  flush_log: false
  max_connection: 0
  disable_ip_origin_check: false
  disable_flood_control: false
  enable_metrics: false
  disable_server_header: false
  enable_h3: false
```

- `binary_path`：镜像内 `hath-rust` 路径。
- `data_dir`：保存 hath-rust 的 cache、data、download、log、tmp 和 `client_login`。
- `log_level`：传给 `hath-rust` 的日志级别；`info`、`warn`、`error`、`off` 分别映射为 `-q`、`-qq`、`-qqq`、`-qqqq`。
- `force_background_scan`：是否启用 `hath-rust` 后台扫描参数，对应 `--force-background-scan`。
- `rpc_server_ip`：需要指定 RPC 服务地址时填写；为空时不传递该参数；非空时对应 `--rpc-server-ip`。
- `disable_logging`：默认 `false`；为 `true` 时传递 `--disable-logging`。
- `flush_log`：默认 `false`；为 `true` 时传递 `--flush-log`。
- `max_connection`：默认 `0`，表示不传递；大于 `0` 时对应 `--max-connection`。
- `disable_ip_origin_check`：默认 `false`；为 `true` 时传递 `--disable-ip-origin-check`。
- `disable_flood_control`：默认 `false`；为 `true` 时传递 `--disable-flood-control`。
- `enable_metrics`：默认 `false`；为 `true` 时传递 `--enable-metrics`。
- `disable_server_header`：默认 `false`；为 `true` 时传递 `--disable-server-header`。
- `enable_h3`：默认 `false`；为 `true` 时传递 `--enable-h3`。

`hath-rust --port` 固定来自 `network.bind_port`。`--cache-dir`、`--data-dir`、`--download-dir`、`--log-dir`、`--temp-dir` 继续从 `hath.data_dir` 派生。`--proxy` 只由 `proxy.url` 与 `proxy.use_for_hath_downloads` 控制。废弃的 `--sni-strict` 不开放，项目也不支持 raw `extra_args`。

## proxy

```yaml
proxy:
  enabled: true
  url: http://127.0.0.1:8080
  use_for_hath_downloads: false
```

- `enabled`：更新 Hentai@Home 设置页时是否走代理。
- `url`：代理地址，支持 `http`、`https` 和 `socks5`。
- `use_for_hath_downloads`：是否让 `hath-rust` 下载缓存时走同一个代理。

当 `proxy.enabled` 或 `proxy.use_for_hath_downloads` 任一为 `true` 时，`proxy.url` 必填；`proxy.url` 不能包含用户名或密码，因为传给 `hath-rust` 的代理参数可能出现在进程命令行中。

使用 `--net host` 时，`127.0.0.1` 指向宿主机网络命名空间。

## bandwidth

```yaml
bandwidth:
  enabled: false
  upload_limit: 10mbit
  interface: eth0
  allow_replace_root_qdisc: false
```

- `enabled`：是否启用上传限速。
- `upload_limit`：上传限速值，支持 `bit`、`kbit`、`mbit`、`gbit`。
- `interface`：应用 `tc` 规则的出口网卡。
- `allow_replace_root_qdisc`：是否允许覆盖目标网卡已有 root qdisc；默认 `false`，避免误覆盖宿主机或外部维护的 `tc` 规则。

启用后会通过 `tc` 对 `network.bind_port` 的出站流量配置上传限速。容器运行时必须授予 `NET_ADMIN` capability。

## runtime

```yaml
runtime:
  shutdown_timeout: 30s
  restart_delay: 5s
  retry:
    initial_delay: 5s
    max_delay: 5m
```

- `shutdown_timeout`：停止 `natmap` 与 `hath-rust` 的最大等待时间。
- `restart_delay`：运行时错误后重新启动 `natmap` 的等待时间。
- `retry.initial_delay`：可重试错误的初始退避时间。
- `retry.max_delay`：可重试错误的最大退避时间。
