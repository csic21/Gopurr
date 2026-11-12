Gopurr 0.6.43 修复 Windows 桌面弹窗的背景遮罩与关闭后不重绘的问题。

- Windows 弹窗的遮罩不再直接模糊窗口后备缓冲：背景不再糊成一片，也不会在对话框周围留下硬边的透明方块和未模糊的卡片描边；macOS 继续使用系统材质模糊。
- Windows 上关闭弹窗后窗口会立即重绘，对话框不再停留在屏幕上，看起来像「窗口无法关闭」的问题已修复；窗口之后继续正常刷新。
- 更新、删除确认、编辑器、组网等全部共享原生弹窗同步生效；弹窗内容与关闭按钮位置不变。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.43-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.43-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.43-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.43-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
