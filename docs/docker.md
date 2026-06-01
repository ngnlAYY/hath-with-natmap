# Docker 运行

本项目只支持 Docker 运行。推荐使用 host network，因为 `natmap` 和 `hath-rust` 需要共享固定本地端口并暴露公网映射。

## 构建

本地构建当前架构镜像：

```bash
docker build -f docker/Dockerfile -t hath:natmap-rust .
```

构建指定平台：

```bash
docker build -f docker/Dockerfile --platform linux/amd64 -t hath:natmap-rust .
```

`docker/Dockerfile` 会在构建期下载固定版本的 `natmap` 与 `hath-rust` release 二进制，并用 `docker/checksums.txt` 校验 SHA256。

## 运行

```bash
docker run --rm \
  --name natmap-rust \
  --net host \
  -e PUID="$(id -u)" \
  -e PGID="$(id -g)" \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath:natmap-rust
```

也可以使用 Docker Compose 示例：

```bash
# 使用已有的 hath:natmap-rust 镜像
docker compose -f docker/docker-compose.yaml up -d

# 或从本地源码构建镜像后运行
docker compose -f docker/docker-compose.build.yaml up -d --build
```

如果启用了 `bandwidth.enabled`，添加 `NET_ADMIN` capability：

```bash
docker run --rm \
  --name natmap-rust \
  --net host \
  --cap-add NET_ADMIN \
  -e PUID="$(id -u)" \
  -e PGID="$(id -g)" \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath:natmap-rust
```

## 权限

- `--net host`：让 `natmap` 和 `hath-rust` 使用宿主机网络栈。
- `--cap-add NET_ADMIN`：仅在启用 `bandwidth.enabled` 时需要。
- `/config/config.yaml`：只读挂载配置文件。
- `/data/hath`：持久化 `hath-rust` 数据。

镜像默认以非 root 用户 `hath` 运行。若挂载宿主机目录到 `/data/hath`，推荐通过 `PUID` 和 `PGID` 指定容器内 `hath` 用户的运行 UID/GID，使其匹配宿主机数据目录所有者：

```yaml
environment:
  PUID: "1000"
  PGID: "1000"
```

也可以直接使用当前宿主机用户：

```bash
-e PUID="$(id -u)" -e PGID="$(id -g)"
```

未设置 `PUID`/`PGID` 时，镜像使用内置的 `hath` 用户和组。`PUID`/`PGID` 必须是非 0 数字。容器启动时会检查 `/data/hath` 和 `/run/hath-natmap` 的属主；只有属主不匹配时才会在当前文件系统内修正权限，且不会跟随符号链接。

## 支持平台

Dockerfile 和 CI 显式支持：

- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`

## GitHub Actions

`.github/workflows/docker-image.yml` 会在同仓库 PR 中构建镜像但不推送；在 `main` 分支 push 时登录 DockerHub 并推送 `taskmgr818/hath-with-natter:latest`。
