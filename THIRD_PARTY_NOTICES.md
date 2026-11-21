# 第三方许可

- Lowpath 源代码、界面及应用图标来自 [omdezo/lowpath](https://github.com/omdezo/lowpath)，MIT 许可。原版权和许可文本见 [LICENSE](LICENSE)。
- Windows 捕获适配层使用 [WinDivert 2.2.2](https://github.com/basil00/WinDivert/releases/tag/v2.2.2)。WinDivert 采用 LGPL v3 或 GPL v2 双重许可，原文见 [WinDivert-LICENSE.txt](internal/divert/bin/WinDivert-LICENSE.txt)。此仓库提供客户端适配源码。构建 Windows amd64 版本前，请从官方发布包取得 x64 的 `WinDivert.dll` 与 `WinDivert64.sys`，放到 `internal/divert/bin/`。
- Go 和前端依赖的版本分别由 `go.mod` 和 `ui/package-lock.json` 固定，各依赖沿用自己的许可证。Inter 字体采用 SIL Open Font License 1.1。
- 游戏名称和通过 Steam 等渠道在运行时读取的封面归各权利人所有。本仓库无游戏安装文件或封面缓存。
- 游戏条目中的 Steam AppID、安装目录和启动项参考 Valve 的 PICS 产品元数据。维护时使用了 MIT 许可的 [node-steam-user](https://github.com/DoctorMcKay/node-steam-user) 和 [SteamCMD API](https://github.com/steamcmd/api) 核对。客户端运行时的游戏库使用随应用提供的目录，并从本机 Steam 安装清单识别已安装游戏。识别方法参考 MIT 许可的 [Playnite Steam 扩展](https://github.com/JosefNemec/PlayniteExtensions/tree/master/source/Libraries/SteamLibrary)。
