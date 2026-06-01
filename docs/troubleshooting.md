# 排障指南

## Hentai@Home 端口一直无法更新

如果日志显示端口字段被锁定，通常表示 `hath-rust` 仍在线或 Hentai@Home 设置页暂时不允许修改端口。编排器会保持 `hath-rust` 停止并重试更新。

还需要检查：

- `ehentai.member_id` 与 `ehentai.pass_hash` 是否对应当前账号。
- `ehentai.client_id` 是否是要更新的 Hentai@Home client。
- `proxy.enabled` 与 `proxy.url` 是否能从容器网络访问 e-hentai。

## natmap 没有公网映射

检查：

- 网络是否为全锥型 NAT。
- `natmap.stun_server` 是否可访问。
- `natmap.http_keepalive_server` 是否可访问。
- 容器是否使用 `--net host`。
- `network.bind_port` 是否被其他进程占用。

## hath-rust 启动失败

检查：

- `/data/hath` 是否可写。
- `ehentai.client_id` 和 `ehentai.client_key` 是否正确。
- `network.bind_port` 是否被其他进程占用。
- `hath.log_level`、`hath.force_background_scan`、`hath.rpc_server_ip` 是否符合当前 `hath-rust` 版本支持的参数。

## 上传限速未生效

检查：

- 配置中 `bandwidth.enabled` 是否为 `true`。
- 容器是否添加 `--cap-add NET_ADMIN`。
- 镜像是否包含 `tc`。
- `bandwidth.interface` 是否为实际出口网卡。
- `tc filter show dev <interface>` 是否存在匹配 `network.bind_port` 的规则。

如果日志提示缺少 `NET_ADMIN capability bounding set`，说明容器启动参数没有授予 `NET_ADMIN`，或运行环境禁用了该 capability。

## 配置校验失败

程序启动时会校验配置。常见原因：

- 必填字段为空。
- `network.bind_port` 不在 1 到 65535 范围内。
- `proxy.enabled` 为 `true` 但 `proxy.url` 为空或协议不受支持。
- `bandwidth.enabled` 为 `true` 但 `upload_limit` 或 `interface` 格式不合法。
- 配置中的外部路径不可执行或目录不可写。

## 代理无法连接

检查：

- `proxy.enabled` 是否符合预期。
- `proxy.url` 是否从容器网络视角可访问。
- host network 下 `127.0.0.1` 指向宿主机网络命名空间。
- 代理是否允许访问 e-hentai 与 Hentai@Home 设置页。

## Docker build 无法连接 Docker socket

如果本地构建出现：

```text
permission denied while trying to connect to the docker API at unix:///var/run/docker.sock
```

说明当前用户没有访问 Docker daemon 的权限。请在宿主机上修复 Docker 权限，或使用有权限的 CI/构建环境。
