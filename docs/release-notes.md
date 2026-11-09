Gopurr 0.6.27 修复组网连接弹窗的布局回退。

- 恢复 SSH、远程桌面和屏幕共享的并排切换，连接方式选项不再跑到窗口底部。
- 恢复居中弹窗、紧凑表单和固定底部操作，长设备名及 840×560 小窗口保持可操作。
- 复制地址、保存设置和打开客户端的结果在弹窗内显示；取消网络准备保留连接草稿。
- 保留订阅、偏好、组网身份及既有更新公钥。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.27-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.27-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.27-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.27-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
