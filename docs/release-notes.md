Gopurr 0.6.24 修复 macOS NSH 组网页面闪退，并校准桌面字体与排版。

- 修复点击组网页面时设备 IP 被序列化为空对象的问题，IPv4、IPv6 正确显示。
- 恢复系统字体的自然行高，分别设置正文、标签和标题字重；调整设置说明、设备和节点名称、规则内容的排版，日志正文恢复等宽字体。
- 保留现有订阅、偏好、组网身份和更新公钥；无需重新导入配置。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.24-arm64.dmg`。
- Windows 10/11 x64：`gopurr-setup-0.6.24-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.24-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.24-android-x86_64.apk`。

桌面也可在「设置 → 维护 → 软件更新」检查更新。Windows 继续使用现有桌面界面；本次不包含独立分支中的 Windows NSH 迁移。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
