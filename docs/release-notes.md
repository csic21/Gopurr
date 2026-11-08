Gopurr 0.6.26 修复 macOS NSH 桌面字号缩小、菜单栏托盘和软件更新反馈。

- 修复 Catalyst 实际文字被缩为约 76.5% 的问题，恢复桌面设计字号。
- 菜单栏图标由前台宿主持有，保留左键显示窗口、右键菜单、系统代理、TUN、路由模式、节点、组网、更新与退出入口；关闭窗口和轻量模式后仍可找回应用。
- 检查更新恢复动态进度条和结果窗口，下载显示真实进度与字节数；支持取消、稍后重启和重试，并遵循减少动态效果设置。
- 保留订阅、偏好、组网身份及既有更新公钥。
- 修复 macOS 更新替换完成后可能未重新启动的问题。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.26-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.26-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.26-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.26-android-x86_64.apk`。

也可在应用内检查更新。沿用 v0.6.25 的 Windows NSH 八页桌面，保留原有 Windows 数据、托盘、快捷键及更新信任链。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
