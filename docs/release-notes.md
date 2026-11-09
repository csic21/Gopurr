Gopurr 0.6.30 为 Android 正式版加入应用内更新。

- Android 设置页新增应用更新：检查 GitHub Releases、查看版本说明、下载进度、取消下载、安装确认和失败重试。
- Release 发布流程生成并上传 `update-android.json`，按 ARM64／x86_64 提供 APK 地址、大小和 SHA-256，并纳入 `SHA256SUMS`。
- APK 安装前校验 SHA-256、应用 ID、版本号、版本名和当前签名证书，再交给 Android 系统 `PackageInstaller`；需要时引导开启“安装未知应用”。
- 调试包不执行应用内更新，正式签名包才显示可用的检查与安装操作。
- 包含 0.6.29 的桌面选择菜单、实时流量动画和搜索空状态修复。
- 保留订阅、偏好、组网身份及既有更新公钥。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.30-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.30-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.30-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.30-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
