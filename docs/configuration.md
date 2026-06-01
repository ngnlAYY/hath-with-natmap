# 配置说明

配置文件默认路径是 `/config/config.yaml`。容器启动时会先校验配置，配置错误会直接终止启动。

可以从示例文件开始：

```bash
cp configs/config.example.yaml config.yaml
```

## ehentai

```yaml
ehentai:
  member_id: "123456"
  pass_hash: "example-pass-hash"
  client_id: "12345"
  client_key: "example-client-key"
```

- `member_id`：e-hentai Cookie `ipb_member_id`。
- `pass_hash`：e-hentai Cookie `ipb_pass_hash`。
- `client_id`：Hentai@Home client ID。
- `client_key`：Hentai@Home client key。

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
```

默认镜像已经包含 `natmap` 和 notify 脚本，通常不需要修改 `binary_path` 与 `notify_script`。

- `stun_server`：用于探测 NAT 映射的 STUN 服务。
- `http_keepalive_server`：用于 keepalive 的 TCP 目标。
- `keepalive_interval`：natmap keepalive 间隔。

## hath

```yaml
hath:
  binary_path: /usr/local/bin/hath-rust
  data_dir: /data/hath
  log_level: info
  force_background_scan: true
  rpc_server_ip: ""
```

- `binary_path`：镜像内 `hath-rust` 路径。
- `data_dir`：保存 hath-rust 的 cache、data、download、log、tmp 和 `client_login`。
- `log_level`：传给 `hath-rust` 的日志级别。
- `force_background_scan`：是否启用 `hath-rust` 后台扫描参数。
- `rpc_server_ip`：需要指定 RPC 服务地址时填写；为空时不传递该参数。

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

使用 `--net host` 时，`127.0.0.1` 指向宿主机网络命名空间。

## bandwidth

```yaml
bandwidth:
  enabled: false
  upload_limit: 10mbit
  interface: eth0
```

- `enabled`：是否启用上传限速。
- `upload_limit`：上传限速值，支持 `bit`、`kbit`、`mbit`、`gbit`。
- `interface`：应用 `tc` 规则的出口网卡。

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
