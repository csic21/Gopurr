Gopurr 0.6.37 修复启用静默启动时，更新重启后窗口转入后台的问题。

- 点击「立即重启」安装更新后，新版本窗口会保持显示，不再闪一下后隐藏。
- 主动更新重启与日常登录静默启动分开处理；登录启动仍遵循「静默启动」设置。
- 同时兼容从 0.6.36 及更早版本升级后的第一次重启，macOS 与 Windows 均已适配。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.37-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.37-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.37-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.37-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
