Gopurr 0.6.68 将代理模式与内置组网路由解耦，并在手机端与共享 SDK 引擎中接入内置 Tailscale 组网支持。

- 代理模式与组网路由解耦：在直连（`direct`）与全局（`global`）模式下保留组网虚拟 IP（`100.64.0.0/10`、`fd7a:115c:a1e0::/48`）的最高路由优先级，切换模式、刷新托盘菜单或关闭系统代理均不影响组网互访。
- 手机端与共享引擎接入内置组网：手机端「设置 → 组网设置」支持连接与登录授权、本机入站端口授权、设备搜索、IP 复制、端口探测与单设备封禁；未导入代理订阅时可直接开启纯组网直连，运行中切换模式或更新组网节点无需重建连接。
- 组网内核防回环与节电适配：`tsnet` 底层套接字接入 `dialer.DefaultSocketHook`（Android `VpnService.protect`），并在窗口隐藏时自动暂停后台组网状态轮询。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.68-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.68-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.68-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.68-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
