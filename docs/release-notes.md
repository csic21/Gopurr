Gopurr 0.6.67 优化了 Windows 构建与发布流程，缩短版本发布所需的等待时间。

- Windows 构建现在复用由 `main` 预热、按依赖哈希缓存的 Cargo/NSH 编译产物，不再在每次 tag 发布时从零编译完整 Rust 依赖树。
- Rust 测试与正式 Windows 构建共享 release 编译产物；Release 只构建一次 Windows 安装包，并直接使用通过迁移、更新、真实内核和 UI 验收的 `windows-nsh` 结果。
- 本次仅调整构建与发布流程，应用功能和已安装版本的数据、配置及升级行为不变。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.67-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.67-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.67-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.67-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
