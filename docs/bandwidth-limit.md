# 上传限速

上传限速通过 Linux `tc` 实现。启用后，容器启动早期会为 `network.bind_port` 对应的出站流量配置 HTB 规则。

## 启用方式

```yaml
bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0
```

运行容器时必须添加：

```bash
--cap-add NET_ADMIN
```

示例配置默认关闭限速，因此默认运行不需要 `NET_ADMIN`。

## 权限模型

镜像默认以非 root 用户运行。Dockerfile 会给镜像内 `tc` 二进制设置 `cap_net_admin+ep`，但容器运行时仍必须通过 `--cap-add NET_ADMIN` 保留 `NET_ADMIN` capability bounding set。若缺少该 capability，程序会在启动阶段用中文错误直接失败。

## 网卡选择

host network 下，`interface` 应配置为宿主机实际出口网卡。可以在宿主机执行：

```bash
ip route get 1.1.1.1
```

输出中的 `dev` 字段通常就是出口网卡。

## 验证规则

```bash
tc qdisc show dev eth0
tc class show dev eth0
tc filter show dev eth0
```

将 `eth0` 替换为配置中的网卡名。

## 清理规则

程序正常退出时会尝试删除目标网卡上的 root qdisc：

```bash
tc qdisc del dev eth0 root
```

如果容器被强制杀死，规则可能残留。可以在宿主机上手动执行同一条命令清理。

## 注意事项

当前规则会在目标网卡上设置 root qdisc。不要在已经手工维护复杂 `tc` 规则的网卡上直接启用此功能。
