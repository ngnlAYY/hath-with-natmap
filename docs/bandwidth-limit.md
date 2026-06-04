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

## root qdisc 保护

启动限速前，程序会先执行：

```bash
tc qdisc show dev eth0
```

以下情况会视为安全并继续执行 `replace`：

- 当前没有 qdisc 输出
- 当前为 `qdisc noqueue`

如果检测到已有 root qdisc，默认会直接失败，避免覆盖宿主机或外部维护的 tc 规则。即使当前 root qdisc 看起来也是 `qdisc htb 1:`，程序也无法可靠证明它由本项目创建，因此仍需要显式开启：

```yaml
bandwidth:
  allow_replace_root_qdisc: true
```

只有在你确认目标网卡上的现有 root qdisc 可以被本程序替换时，才应启用这个开关。完整示例：

```yaml
bandwidth:
  enabled: true
  upload_limit: 10mbit
  interface: eth0
  allow_replace_root_qdisc: true
```

## 清理规则

程序正常退出时只会在本进程已经成功应用限速，且当前 root qdisc 仍然是带有本项目专用 `default 3fed` 标记的 `qdisc htb 1:` 时，才尝试删除：

```bash
tc qdisc del dev eth0 root
```

如果检测到外部 root qdisc，或当前 runner 无法确认 qdisc 归属，程序会跳过删除，避免误删其它 tc 规则。

如果容器被强制杀死，规则可能残留。可以在宿主机确认归属后再手动执行同一条命令清理。

## 注意事项

当前规则会在目标网卡上设置 root qdisc。默认保护只能减少误覆盖风险，不能替代你对宿主机 tc 规则现状的确认。
