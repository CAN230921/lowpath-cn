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

`-gateway` 可通过环境变量 `LOWPATH_GATEWAY` 提供。macOS 上可用 `-uplink auto` 让客户端的新 HTTPS 和 MASQUE 连接自动选用物理网卡，或用 `-uplink en0` 指定网卡。`-uplink=""` 使用系统路由。`-gateway-ip` 固定实际连接的 IPv4，可避开 TUN 返回的虚拟 DNS 地址，TLS 仍按网关域名验证证书。账号由网关管理员开通。macOS 构建启用 CGO 时，选中「在这台设备上保持登录」可在 30 天内重新打开客户端并恢复登录。钥匙串存储续期凭据，主动退出登录时清除并向网关撤销会话。

## macOS

启用 CGO 构建时，Mac 客户端通过 CoreWLAN 读取 Wi-Fi 信号、频段、信道和发送速率，并从 macOS `.app` 读取本机游戏图标。加速前的服务器识别通过系统的连接统计读取 TCP 和已连接 UDP 套接字的活动。信道调查读取系统已有的扫描缓存，可能需要定位授权。macOS 没有 Lowpath 在 Windows 上使用的扫描暂停和串流模式开关。

Mac 本机版可以用 `codesign --force --sign - "Lowpath CN.app"` 自签。自签包支持账号、游戏库、诊断和指定目标的代理测试。按游戏进程接管 IPv4 UDP 的网络扩展要求 macOS 15 或更新版本，以及 Apple 授予的网络扩展权限。用 Developer ID 分发时，先构建桌面程序和 `.app`，再对暂存应用运行以下命令。

```sh
scripts/macapp/sign-routing.sh 'Lowpath CN.app' 'Developer ID Application: …' TEAM_ID App.provisionprofile Route.provisionprofile
```

主应用和后缀为 `.routing` 的扩展需使用同一团队的 Developer ID 配置文件与同一应用组 `TEAM_ID.<主应用 Bundle ID>`。打包后按 Apple 流程公证，将应用放在「应用程序」目录。首次开始路由会话时在系统设置中批准扩展。扩展已编译并测试本地收发桥接，系统激活和真实游戏流量仍需具备权限的签名包验证。

### Mac UDP 测试

使用自有的 UDP 回显服务器作为测试目标。启动后登录客户端，选中「UDP 线路实测」，选择已授权节点并点击「加速」。

```sh
./accelerator -gateway https://edge.example.org -test-target 203.0.113.12:42319
```

代理模式下，将测试程序连接到界面显示的本地 UDP 地址。该模式已在 Mac 上通过公网 MASQUE 和 AWS 验证。开发用 `-test-route` 需要管理员权限和与测试目标不同的 `-gateway-ip`，以临时 utun 主机路由接管指定测试 IPv4 地址和 UDP 端口。

## Windows x64

在 [GitHub Actions](https://github.com/CAN230921/lowpath-cn/actions/workflows/windows.yml) 打开最近一次成功的构建，下载并解压 `Lowpath-Windows-x64`，运行 `Lowpath.exe`。适用于 Windows 10/11 x64。

构建预填香港托管网关，首次打开时用管理员发放的账号登录。路由模式使用内置的 WinDivert 驱动，需要 Windows 管理员权限。可直接右键以管理员身份启动，或在应用里按提示授权并重启。桌面窗口使用 WebView2，缺少运行时时会在默认浏览器打开界面。

关闭或最小化桌面窗口后，Lowpath 默认继续在系统托盘运行和加速。点击托盘图标恢复窗口，右键选择「退出」停止加速并结束客户端。可在「设置 → 通用」关闭驻留。

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
