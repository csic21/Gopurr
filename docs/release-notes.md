Gopurr 0.6.53 修复桌面侧栏当前页选中态。

- 当前页的浅蓝选中底色与左缘蓝色竖线不再依赖悬停，切页后持续显示。
- 选中面改由导航的原生滑动指示层独占绘制，消除重复背景遮挡并恢复单层选中动画。
- 未选中导航仍保留悬停反馈；键盘选中语义、快速切换与系统「减少动态效果」不变。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.53-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.53-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.53-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.53-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
