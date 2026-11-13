Gopurr 0.6.44 在内置组网设备详情中显示加密中继的 DERP 区域代号，并优化 macOS 发布构建磁盘空间。

- 组网设备详情的「当前链路」在走加密中继时显示具体区域代号（如 `加密中继 (tok)` 或自定义区域代号），便于确认实际中继节点；默认配置与连接逻辑不变。
- 修复 macOS 正式发布构建磁盘空间不足的问题。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.44-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.44-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.44-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.44-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
