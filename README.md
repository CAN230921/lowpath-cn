# Lowpath CN

Lowpath 的简体中文桌面界面，以及连接托管节点的 Go 客户端和 MASQUE 网关。

客户端通过 HTTPS 获取设备可访问的节点，经 HTTP/3 CONNECT-UDP 转发游戏 UDP。客户端到网关使用 QUIC 和 TLS 1.3。网关到后端转发节点仍使用 Lowpath UDP。GitHub OAuth 可由网关换发限时设备凭据，已有设备凭据也可以直接登录。

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

`-gateway` 可通过环境变量 `LOWPATH_GATEWAY` 提供。macOS 如需绑定接入网卡，可以指定 `-uplink en0`。`-gateway-ip` 固定实际连接的 IPv4，TLS 仍按网关域名验证证书。设备凭据保存在本次客户端进程内，退出后需再次登录。

### Mac UDP 测试

使用自有的 UDP 回显服务器作为测试目标。启动后登录客户端，选中「UDP 线路实测」，选择已授权节点并点击「加速」。

```sh
./accelerator -gateway https://edge.example.org -test-target 203.0.113.12:42319
```

代理模式下，将测试程序连接到界面显示的本地 UDP 地址。开发用 `-test-route` 需要管理员权限和与测试目标不同的 `-gateway-ip`，以临时 utun 主机路由接管专用测试服务器。该模式承载指定端口的 IPv4 UDP，停止或退出会关闭测试接口，进程最迟 90 秒退出。

Windows 客户端计划复用同一套 React 界面和托管节点 API。参阅 [第三方许可](THIRD_PARTY_NOTICES.md) 获取可选的 Windows 流量捕获依赖。

## 部署网关与启用 OAuth

在 `gateway/` 构建 `./cmd/edge` 和 `./cmd/access`，参考 `gateway/units/` 的 systemd 模板。网关使用 `EDGE_DOMAIN`、systemd 状态目录及凭据目录。通过 `EDGE_DEVICE_DIR` 启用托管节点，systemd 凭据 `routes` 提供节点和中继配置。

- 客户端向 `GET /api/v1/nodes` 和 CONNECT-UDP 发出设备 Bearer 凭据。网关校验有效期和节点权限，并限制并发隧道。
- 启用 GitHub 登录时，将 GitHub App 或 OAuth App 的回调设置为 `https://edge.example.org/api/v1/login/github/callback`。登录只需识别 GitHub 账号，用 GitHub 数字用户 ID 明确分配可用节点。
- 在网关的 systemd drop-in 添加 `LoadCredential=github-oauth:/etc/lowpath-masque/github-oauth.json`。该文件由管理员保管，结构如下。GitHub Client Secret 只由网关读取。

```json
{
  "client_id": "YOUR_GITHUB_CLIENT_ID",
  "client_secret": "YOUR_GITHUB_CLIENT_SECRET",
  "accounts": {"123456": ["tokyo"]}
}
```

GitHub 授权使用 PKCE 和一次性状态值。网关换发 12 小时的设备凭据，退出登录可撤销 OAuth 签发的凭据。OAuth 配置是可选项，设备凭据登录可以独立使用。

## 许可

本项目基于 [omdezo/lowpath](https://github.com/omdezo/lowpath) 的 `fbfdb7c77528e2761b47b53e3429f4b6ad8f594a`。源代码沿用 [MIT 许可](LICENSE)。第三方组件的许可见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
