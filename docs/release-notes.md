Gopurr 0.6.58 修复大量节点测速时只测到前 100 个的问题。

- 「测试本组」和「测试全部」会测完当前可测节点，后面的节点不再一直显示未测试。
- 同时测多少个仍由设置里的并发数决定，可选 1 到 32，默认 8。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.58-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.58-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.58-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.58-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
