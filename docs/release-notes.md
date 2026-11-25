Gopurr 0.6.72 将原生界面运行时升级到 NSH 0.7.0，并修复升级后暴露的弹层层级与原生选择器焦点问题。

- NSH 0.4.0 → 0.7.0：更新 macOS/Windows CLI 固定版本、校验和与 Cargo revision；Apple/Android 嵌入 SDK 改为独立资产下载和校验。桌面与手机 bundle 均重新构建。
- 适配 NSH 0.7 的能力和窗口生命周期：桌面应用显式授予 `application` 能力；Windows 继续使用原来的应用级数据目录，升级不会把界面偏好迁到新的存储位置。
- 修复移动端订阅操作面板未关闭就切换页面造成的点击遮挡；“准备访问”确认框现在位于连接设置上方；桌面下拉选择器恢复 0.7 的指针焦点行为，同时保留键盘导航。
- 保留现有 Windows 对话框重绘、Win32 纯色遮罩和 Catalyst 字体缩放兼容处理，待实机截图验收后再单独评估。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.72-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.72-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.72-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.72-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
