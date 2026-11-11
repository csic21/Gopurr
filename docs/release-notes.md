Gopurr 0.6.38 修复系统服务的控制套接字被系统清理后内核永久失联、启动失败提示误导的问题。

- 内核每 5 秒校验私有与已发布的控制套接字，连续两次失效即正常退出，由 launchd 或应用看门狗用全新套接字重启；不再出现「内核已停止」而旧内核仍占用混合端口与 TUN 路由的状态。
- 「启动失败」不再引用安装时拷贝的旧日志：过期内容会标注「早于本次启动」，并跳过 `[TUN] default interface lost by monitor` 这类每次网络切换都会出现的无害日志。
- 控制套接字失效时，macOS「重启服务」改用 `launchctl kickstart -k`（一次管理员授权替换守护进程）；套接字正常时仍走无需授权的原地重启。
- 系统服务接口版本落后时立即提示「修复服务…」，不再等待 30 秒超时；安装脚本会清除上次运行遗留的 `core.json` 与 `core.sock`。
- 已安装 0.6.10 及更早服务的用户需在网络详情点一次「修复服务…」；订阅与设置保留，完成后普通应用更新不再要求重装服务。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.38-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.38-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.38-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.38-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
