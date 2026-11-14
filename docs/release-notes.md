Gopurr 0.6.47 优化 Windows 前端与界面刷新。

- Windows 界面进程不再加载 Go bridge DLL。启动 Go 业务后端和转发本地 RPC 由 Rust 宿主完成，后台、内核与系统服务保持独立运行。
- 降低可见页面的空闲重渲染：每秒快照会复用未变化的数据，非概览页忽略实时流量、连接数和历史采样。代理列表、弹窗和编辑状态不再因为后台刷新而无谓重建。
- 新增性能诊断日志，便于验证实际帧率和掉帧情况。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.47-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.47-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.47-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.47-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
