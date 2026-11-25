Gopurr 0.6.71 修复 Android 应用内自动更新无法走本地代理、底部导航栏切换菜单时原先菜单出现深色闪烁动画的问题，并为手机端核心交互补充原生系统触觉反馈。

- Android 应用内更新走本地代理：更新清单检查与 APK 下载优先复用运行中的本地 HTTP 代理端口（`127.0.0.1:<mixedPort>`），端口并发关闭时自动回退直连；冷启动未连代理导致的首次自动检查失败会在代理 VPN 连接成功后自动重查一次。
- 修复底部导航切换闪烁：未选中标签改用导航栏同色不透明底色并去除透明按钮的背景色过渡插值，消除 Android 非预乘透明黑中间帧造成的深色闪动。
- 原生 UX/UE 触觉反馈：通过 Android 系统 `View.performHapticFeedback`（无需额外震动权限且遵循系统触感开关）为标签/分段切换、开关、连接与刷新触发、保存/复制/连接成功确认以及错误或破坏性操作拒绝提供细腻触感反馈。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.71-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.71-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.71-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.71-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
