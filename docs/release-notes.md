Gopurr 0.6.48 恢复桌面侧栏的收展动画。

- 点击标题栏侧栏按钮后，侧栏滑出、品牌迁移、八个导航项移动到标题栏，按钮跟随同一动画进度。
- 页面卡片仍只做一次布局提交，不在整页逐帧重排，避免切换过程中露出窗口底色和卡片缝隙。
- 支持快速反向切换与系统「减少动态效果」；包含 0.6.47 的 Windows 前端与界面刷新改进。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.48-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.48-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.48-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.48-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
