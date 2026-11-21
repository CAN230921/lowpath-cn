# Lowpath CN

Lowpath 的简体中文桌面界面，以及连接托管节点的 Go 客户端和 MASQUE 网关。

客户端通过 HTTPS 登录并获取可访问节点，经 HTTP/3 CONNECT-UDP 转发游戏 UDP。客户端到网关使用 QUIC 和 TLS 1.3。网关将账号密码验证换成限时、按节点授权的设备凭据。

## 构建客户端

需要 Go 1.26.4 和 Node.js。

```sh
cd ui
npm ci
npm run build
cd ..
go build -o accelerator ./cmd/accelerator
./accelerator -gateway https://edge.example.org
```

`-gateway` 可通过环境变量 `LOWPATH_GATEWAY` 提供。macOS 如需绑定接入网卡，可以指定 `-uplink en0`。`-gateway-ip` 固定实际连接的 IPv4，TLS 仍按网关域名验证证书。设备凭据保存在本次客户端进程内，退出后需再次登录。账号由网关管理员开通。

### Mac UDP 测试

使用自有的 UDP 回显服务器作为测试目标。启动后登录客户端，选中「UDP 线路实测」，选择已授权节点并点击「加速」。

```sh
./accelerator -gateway https://edge.example.org -test-target 203.0.113.12:42319
```

代理模式下，将测试程序连接到界面显示的本地 UDP 地址。该模式已在 Mac 上通过公网 MASQUE 和 AWS 验证。开发用 `-test-route` 需要管理员权限和与测试目标不同的 `-gateway-ip`，以临时 utun 主机路由接管专用测试服务器。系统路由模式仍在验收，设计范围为专用测试 IPv4 地址和指定的 UDP 端口。

Windows 客户端计划复用同一套 React 界面和托管节点 API。参阅 [第三方许可](THIRD_PARTY_NOTICES.md) 获取可选的 Windows 流量捕获依赖。

## 部署网关与创建账号

在 `gateway/` 构建 `./cmd/edge` 和 `./cmd/access`，参考 `gateway/units/` 的 systemd 模板。网关使用 `EDGE_DOMAIN`、systemd 状态目录及凭据目录。通过 `EDGE_DEVICE_DIR` 启用托管节点，systemd 凭据 `routes` 提供节点和中继配置。

- 启用账号登录时，在网关创建只供服务读取的系统组 `lowpath-auth`，以及 root 所有的 setgid 账号目录。将 `gateway/units/lowpath-masque-accounts.conf` 放入服务的 systemd drop-in 目录，重载后重启网关。
- 新账号只能通过管理员终端创建。密码至少 8 个字符、最多 256 字节。命令会隐藏密码输入，网关只保存 Argon2id 密码散列和允许访问的节点。
- 登录后，客户端向 `GET /api/v1/nodes` 和 CONNECT-UDP 发送有效期 12 小时的设备凭据。退出登录可撤销该设备会话。

```sh
sudo groupadd --system lowpath-auth
sudo install -d -m 2750 -o root -g lowpath-auth /etc/lowpath-access/accounts
sudo lowpath-access create-user -username player -nodes tokyo
```

## 许可

本项目基于 [omdezo/lowpath](https://github.com/omdezo/lowpath) 的 `fbfdb7c77528e2761b47b53e3429f4b6ad8f594a`。源代码沿用 [MIT 许可](LICENSE)。第三方组件的许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
