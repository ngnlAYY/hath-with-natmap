# Docker 运行

本项目只支持 Docker 运行。推荐使用 host network，因为 `natmap` 和 `hath-rust` 需要共享固定本地端口并暴露公网映射。

## 构建

本地构建当前架构镜像：

```bash
docker build -f docker/Dockerfile -t hath-with-natmap:local .
```

构建指定平台：

```bash
docker build -f docker/Dockerfile --platform linux/amd64 -t hath-with-natmap:local .
```

`docker/Dockerfile` 会在构建期下载固定版本的 `natmap` 与 `hath-rust` release 二进制，并用 `docker/checksums.txt` 校验 SHA256。

## 运行

```bash
docker run --rm \
  --name hath-with-natmap \
  --net host \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath-with-natmap:local
```

如果启用了 `bandwidth.enabled`，添加 `NET_ADMIN` capability：

```bash
docker run --rm \
  --name hath-with-natmap \
  --net host \
  --cap-add NET_ADMIN \
  -v "$PWD/config.yaml:/config/config.yaml:ro" \
  -v "$PWD/hath:/data/hath" \
  hath-with-natmap:local
```

## 权限

- `--net host`：让 `natmap` 和 `hath-rust` 使用宿主机网络栈。
- `--cap-add NET_ADMIN`：仅在启用 `bandwidth.enabled` 时需要。
- `/config/config.yaml`：只读挂载配置文件。
- `/data/hath`：持久化 `hath-rust` 数据。

镜像默认以非 root 用户 `hath` 运行。若挂载宿主机目录到 `/data/hath`，请确保容器内用户可以写入该目录。

## 支持平台

Dockerfile 和 CI 显式支持：

- `linux/amd64`
- `linux/arm64`
- `linux/arm/v7`

## GitHub Actions

`.github/workflows/docker-image.yml` 会在同仓库 PR 中构建镜像但不推送；在 `main` 分支 push 时登录 DockerHub 并推送 `taskmgr818/hath-with-natter:latest`。
