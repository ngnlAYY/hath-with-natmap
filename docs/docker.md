# Docker 运行

本项目只支持 Docker 运行。推荐使用 host network，因为 `natmap` 和 `hath-rust` 需要共享固定本地端口并暴露公网映射；如果使用 `mapping.mode: upnp`，同样推荐 `--net host`，因为容器需要在宿主机所在局域网中发现路由器的 UPnP/IGD 服务。

## 构建

本地构建当前架构镜像：

```bash
docker build -f deploy/docker/Dockerfile -t hath:natmap-rust .
```

构建指定平台：

```bash
docker build -f deploy/docker/Dockerfile --platform linux/amd64 -t hath:natmap-rust .
```

`deploy/docker/Dockerfile` 会在构建期下载固定版本的 `natmap` 与 `hath-rust` release 二进制，并用 `deploy/docker/checksums.txt` 校验 SHA256。

## 运行

```bash
mkdir -p hath
chown -R 1000:1000 hath

docker run --rm \
  --name natmap-rust \
  --net host \
  --user 1000:1000 \
  --tmpfs /run/hath-natmap:uid=1000,gid=1000,mode=700 \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath:natmap-rust
```

也可以使用 Docker Compose 示例：

```bash
# 使用已有的 hath:natmap-rust 镜像
docker compose -f deploy/docker/docker-compose.yaml up -d

# 或从本地源码构建镜像后运行
docker compose -f deploy/docker/docker-compose.build.yaml up -d --build
```

`mapping.mode: upnp` 本身不需要 `NET_ADMIN`；只有启用了 `bandwidth.enabled` 时，才需要额外添加 `NET_ADMIN` capability：

```bash
docker run --rm \
  --name natmap-rust \
  --net host \
  --cap-add NET_ADMIN \
  --user 1000:1000 \
  --tmpfs /run/hath-natmap:uid=1000,gid=1000,mode=700 \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath:natmap-rust
```

## 权限

- `--net host`：让 `natmap` 和 `hath-rust` 使用宿主机网络栈；`mapping.mode: upnp` 时也推荐开启，便于在宿主机所在局域网中发现路由器 UPnP/IGD。
- `--cap-add NET_ADMIN`：仅在启用 `bandwidth.enabled` 时需要；UPnP 映射本身不依赖这个 capability。
- `/config/config.yaml`：只读挂载配置文件。
- `/data/hath`：持久化 `hath-rust` 数据。
- `--user 1000:1000`：直接以镜像内固定的非 root `hath` 用户运行主进程。
- `--tmpfs /run/hath-natmap:uid=1000,gid=1000,mode=700`：为 natmap notify socket 提供仅当前运行用户可写的运行时目录。

镜像不会在启动时递归修改宿主机挂载目录权限。首次运行前请在宿主机上创建数据目录，并确保 UID/GID `1000:1000` 可写：

```bash
mkdir -p hath
chown -R 1000:1000 hath
```

如果需要使用其它宿主机 UID/GID，请同时调整 `--user`、`--tmpfs` 的 `uid/gid`，并将数据目录属主改为同一 UID/GID。若通过 `HATH_NATMAP_NOTIFY_SOCKET` 自定义 notify socket 路径，建议将父目录设置为当前运行用户独占，避免其他本地用户删除或占用 socket 文件。

## 支持平台

当前优化范围只覆盖 Linux x86-64：

- `linux/amd64`

`linux/arm64`、`linux/arm/v7` 和非 Linux 平台暂不作为当前发布目标。

## GitHub Actions

`.github/workflows/docker-image.yml` 会在同仓库 PR 中构建镜像但不推送；在 `main` 分支 push 时登录 DockerHub 并推送 `taskmgr818/hath-with-natter:latest`。
