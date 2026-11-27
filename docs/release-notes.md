Gopurr 0.6.74 修复移动端节点页底部空白与编辑页保存栏内边距，并为 PC 与移动端节点编辑器新增 Reality ML-KEM-768 后量子密钥交换开关与嵌套子参数保留能力。

- 修复移动端节点页底部空白（将 7 个关闭态 `BottomSheet` 移出带 `gap` 的外层容器），并同步排查修正桌面端同级 `Modal` 与移动端节点/策略组编辑页底部保存栏在收起键盘时的内边距。
- PC 与移动端节点编辑器的 Reality 配置新增 `support-x25519mlkem768`（ML-KEM-768 后量子密钥交换）开关，并改进 `reality-opts` / `ws-opts` / `grpc-opts` / `h2-opts` 的嵌套字段合并逻辑，编辑保存时完整保留未知子参数不被覆盖。

下载：

- macOS 12 及以上 Apple Silicon：`gopurr-0.6.74-arm64.dmg`。
- Windows 10 1809 及以上／Windows 11 x64：`gopurr-setup-0.6.74-amd64.exe`。
- Android 9 及以上 ARM64：`gopurr-0.6.74-android-arm64-v8a.apk`。
- Android x86_64：`gopurr-0.6.74-android-x86_64.apk`。

也可在应用内检查更新。

发布包暂未配置 Apple Developer ID／公证及 Windows Authenticode 证书，系统可能提示无法验证开发者。请从本仓库下载，并通过 SHA256SUMS 核对文件。Android 正式 APK 沿用固定证书，可覆盖已有正式版本。
