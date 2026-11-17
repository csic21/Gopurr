Gopurr 0.6.59 修复 macOS 开启 TUN 后，不走代理端口的程序访问 HTTPS 一直等到超时。

- macOS 的 TUN 出厂协议栈改为 gvisor，TCP 在用户态结束，不再经过会丢掉 TLS 数据的内核改写。
- 已经存成出厂 mixed 的配置会在下次启动时改成 gvisor，只改这一次。设置里仍可以手动选择 mixed 或 system。Windows 默认不变。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.59-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.59-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.59-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.59-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
