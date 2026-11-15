Gopurr 0.6.50 修复桌面侧栏动画收尾时的 LOGO 抖动。

- 保留 0.6.49 的原轨迹：LOGO 从左上角移到标题栏，图标由 30 缩到 24，文字由 15 缩到 13，时长 320ms 缓入缓出。
- 动画最后一帧不再重新挂载真实 LOGO；图标和文字的飞行终点与静止落点一致，消除收尾抖动。
- 页面卡片仍只做一次布局提交；保留快速反向与系统「减少动态效果」。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.50-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.50-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.50-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.50-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
