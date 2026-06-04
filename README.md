# hath-with-natmap

`hath-with-natmap` 是一个 Docker-only 编排器，用于在全锥型 NAT 网络中运行 `hath-rust`。它使用 `natmap` 获取公网映射端口，自动更新 Hentai@Home 设置页，并在需要时启动或重启 `hath-rust`。

## 特性

- 使用 `natmap` bind 模式获取公网映射。
- `natmap` 与 `hath-rust` 共享固定本地端口。
- 公网映射未变化时不重复更新 Hentai@Home，也不重启正在运行的 `hath-rust`。
- 首次获取映射或公网端口变化时自动更新 Hentai@Home 端口。
- 可配置跳过访问 Hentai@Home 设置页更新公网端口。
- 可选自动配置 Linux `tc` 上传限速。
- 可选使用路由器 UPnP/IGD 添加固定 TCP 端口映射。
- 镜像内置 `hath-natmap`、`natmap`、`hath-rust` 和 notify 脚本。
- 中文日志、中文错误信息和中文文档。

## 快速开始

复制配置文件：

```bash
cp configs/config.example.yaml config.yaml
```

编辑 `config.yaml`，填入 e-hentai Cookie、Hentai@Home client 信息、固定端口、代理和限速配置。如果路由器支持 UPnP/IGD，可以将 `mapping.mode` 设置为 `upnp`；否则保持默认 `natmap`。

构建镜像：

```bash
docker build -f docker/Dockerfile -t hath:natmap-rust .
```

运行容器：

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

如果启用了 `bandwidth.enabled`，需要额外添加：

```bash
--cap-add NET_ADMIN
```

也可以使用 Docker Compose 示例：

```bash
# 使用已有的 hath:natmap-rust 镜像
docker compose -f docker/docker-compose.yaml up -d

# 或从本地源码构建镜像后运行
docker compose -f docker/docker-compose.build.yaml up -d --build
```

## 文档

- [配置说明](docs/configuration.md)
- [Docker 运行](docs/docker.md)
- [上传限速](docs/bandwidth-limit.md)
- [排障指南](docs/troubleshooting.md)

## 参考项目

- [heiher/natmap](https://github.com/heiher/natmap)
- [james58899/hath-rust](https://github.com/james58899/hath-rust)
