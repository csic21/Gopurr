Gopurr 0.6.32 修复桌面界面长时间运行后内存持续上涨。

- 修复每秒刷新状态时，订阅、策略组和访问记录等列表被整表重建，macOS 窗口内存涨到约 1.1 GB 的问题。列表按稳定身份复用原行，内容和选中状态仍实时更新。
- 已经涨上去的内存不会自动退回，重新打开窗口后恢复正常。
- 保留 0.6.31 的软件更新弹窗稳定性修复，以及既有订阅、偏好、组网身份和更新公钥。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.32-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.32-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.32-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.32-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
