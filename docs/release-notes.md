Gopurr 0.6.31 修复桌面软件更新弹窗持续闪烁。

- 修复状态每秒刷新时更新弹窗被反复重建、持续播放淡入动画的问题；弹窗显示状态未改变时保持原生弹层稳定。
- 更新说明和下载进度继续实时刷新，保留检查、取消、稍后、下载、重试与重启操作。
- 增加跨桌面平台的连续刷新、关闭及重新打开回归，并验证长版本说明、1060×720／840×560 窗口和明暗主题。
- 保留 0.6.30 的 Android 正式版应用内更新及安装前校验、桌面选择菜单、实时流量动画和搜索空状态修复。
- 保留订阅、偏好、组网身份及既有更新公钥。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.31-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.31-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.31-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.31-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
