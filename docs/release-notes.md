Gopurr 0.6.26 将 Windows 正式桌面迁移到 NSH，现有 Windows 正式版可直接在应用内更新。

- 修复 macOS 更新已安装完成但应用未自动重启的问题。
- Windows 使用与 macOS 共用的八页原生桌面，支持明暗主题、折叠导航、托盘后台运行、快捷键、自启动和静默启动。
- 订阅、凭据、规则、设置和组网身份保留在原 `%APPDATA%\Gopurr` 目录，首次接管保存私有迁移快照。
- 继续使用原更新公钥。旧版与 NSH 更新入口下载同一个完整签名包，包含新版安全卸载器；升级和卸载均保留用户数据。
- 修复原生事件轮询可能等待连接关闭的问题；正式发布前验证实际 Windows 宿主的旧版升级、再次升级重启、真实代理流量、安装/卸载和服务能力。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.26-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.26-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.26-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.26-android-x86_64.apk`。

桌面可在「设置 → 维护 → 软件更新」检查更新。应用更新包使用原 Ed25519 公钥验签；Windows NSH 验收、macOS 更新验收和 Android 签名构建全部通过后才发布。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
