Gopurr 0.6.36 修复桌面开关的选中态显示与过渡动画。

- 开关开启后，滑块现在会移动到轨道右侧，不再只改变轨道颜色。
- 轨道与滑块共用 240 ms 过渡；快速连续切换从当前画面继续，减少动态效果时直接显示最终状态。
- 设置、概览、组网等页面共用同一修复，并补充滑块位移回归测试和双尺寸明暗截图。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.36-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.36-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.36-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.36-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
