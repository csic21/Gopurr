Gopurr 0.6.46 修复 Windows 托盘图标缺失。

- 0.6.41 移除旧窗口工具包后，Windows 通知区没有创建托盘图标；原生前端现在重新接管托盘。
- 浅色和深色任务栏分别使用对应图标；左键或双击显示窗口，右键打开实时状态菜单。
- 桌面资源管理器重启后自动恢复图标，并保留原托盘图标编号。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.46-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.46-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.46-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.46-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
