Gopurr 0.6.39 修复桌面侧栏切换掉帧，并统一有限动画到 NSH 原生动画时钟。

- 侧栏从 320ms 的逐帧 JavaScript 重排改为 NSH 原生 `LayoutAnimation`，一次提交完成；快速反向与系统「减少动态效果」保持可用。
- 标题栏侧栏按钮补充原生按下反馈；概览出口文字、软件更新进度条和手机页切换改用 `createAnimatedValue`，动画继续在 NSH 原生时钟上运行。
- 流量图的真实采样曲线仍在 Canvas 内做最多 400ms 的受控插值，不改变布局；其余有限动画不再逐帧写入 JS 样式。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.39-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.39-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.39-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.39-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
