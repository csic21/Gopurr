Gopurr 0.6.41 去掉旧窗口工具包，桌面托盘和更新重启改由原生宿主完成。

- 托盘菜单、文件选择、剪贴板、打开链接以及更新后的重启不再经过旧界面库。
- 命令行直接启动不再打开旧窗口；请继续从应用包使用桌面。
- 更新公钥不变。已安装的版本可以正常检查并安装这个版本。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.41-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.41-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.41-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.41-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
