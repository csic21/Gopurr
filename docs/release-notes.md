Gopurr 0.6.40 修复桌面侧栏切换的卡顿和空白，并让 Windows 托盘双击打开窗口。

- 侧栏按钮改为 180ms 原生位移动画，页面布局一次到位，不再逐帧重排整棵卡片树，因此切换时不会在卡片之间或窗口边缘留下白块。
- 窗口底色与桌面画布一致，减少动态效果时切换立即完成。
- Windows 托盘双击与单击一样显示窗口。之前双击消息被忽略，习惯双击托盘图标时窗口不会出现。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.40-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.40-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.40-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.40-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
