# NSH 原生客户端

这是 Gopurr 的 NSH 原生客户端。macOS 与 Windows 正式构建和发布入口使用八页 NSH 桌面，手机继续 Android 优先。NSH 固定为 **0.4.0 / e626a8d**，SDK 下载到仓库内 `.cache/nsh` 并验证 SHA-256，不全局安装工具。迁移设计和已运行验证见 [macOS 验收记录](MACOS_MIGRATION.md) 与 [Windows 验收记录](WINDOWS_MIGRATION.md)。

## 结构

```text
native/ui             Desktop.tsx 八页桌面；App.tsx 四页手机
native/android        Java Activity、VpnService 和 JNI
native/apple          Swift / UIKit 宿主（Mac Catalyst、iOS Simulator）
cmd/gopurr-bridge     Go C ABI；原生宿主调用，不开放 HTTP 控制端口
sdk/engine            手机进程内业务接口，复用 profile、patch、mihomo
internal/workflow     桌面/手机共享配置提交、订阅导入更新与失败恢复
internal/app/native_*  桌面完整业务协议，复用现有事务、系统服务与恢复逻辑
internal/nativehost   私有 Unix socket、会话认证和托盘/快捷键事件
```

手机业务调用 `host.call("engine.dispatch", {method, params})`。Android 用 `NshView.handle` 接入 JNI；Apple 用 `NshAppView.handle` 接入 C archive。Android 连接意图由进程级 SessionController 管理，状态查询、配置下载和连接控制分开调度；配置写入与内核应用仍保持串行，耗时下载/测速不阻塞 UI 状态和断开。

桌面界面调用 `desktop.dispatch`，Swift 宿主通过 C ABI 启动包内 `gopurr-desktop --native-backend`。它保留原桌面的业务模型、托盘、全局快捷键、特权服务与独立内核进程，但不创建旧窗口。每次会话使用随机认证令牌、0700 目录与 0600 Unix socket，不开放额外 TCP 控制口。文件选择、剪贴板与外部客户端调用通过单独授权的宿主通道。

## 构建

当前 bootstrap 支持 macOS Apple Silicon。Go 缓存由 Makefile 统一放在 `.cache`；仅修改 NSH TSX 时不需要 Node 或 Rust 编译器。

```sh
make native-tools         # 下载、校验固定 NSH 工具链
make native-check         # NSH 类型检查；已有 tsc 时优先使用
make native-test          # Go 业务测试 + NSH 交互测试
make native-smoke         # 手机 C ABI + 本地 SOCKS/HTTP 流量验证
make native-desktop-smoke # 桌面 helper + 真实内核事务/回滚/备份/退出验证
make native-android       # Android arm64 开发 APK
make native-android-release # Android 正式签名 APK；需要专用签名环境变量
make native-android-test  # 隔离模拟器上的实际 JNI / VPN / 路由规则验证
make native-bundle        # Swift Mac Catalyst 应用包
make native-run           # 构建并打开 Mac Catalyst 预览
make bundle               # 正式身份的 macOS NSH 应用包（不发布）
```

Android 构建需要 JDK 17、Android SDK 36、Build Tools 36.1.0 和 NDK。可通过 `ANDROID_SDK_ROOT`、`ANDROID_NDK_HOME`、`ANDROID_BUILD_TOOLS`、`GOPURR_JAVA_HOME` 指定已有安装。`NATIVE_ANDROID_ABI=x86_64` 可构建对应模拟器 APK。

Android 专用构建启用 mihomo 的 `cmfa` 嵌入模式及 `with_gvisor`。Android 包选择和系统路由由 `VpnService.Builder` 管理，不使用独立命令行核心中需要 root 的系统包清单读取路径；桌面构建标签保持原样。

产物：

- `bin/native/gopurr-native-arm64-v8a-debug.apk`
- `bin/native/Gopurr Native.app`
- `bin/native/ios-simulator/Gopurr Native.app`（`make native-bundle NATIVE_TARGET=ios-simulator`）

`make native-android` 使用 `.cache/nsh/android-debug.keystore` 的开发签名，仅用于开发验证。普通 CI 构建 arm64-v8a 和 x86_64 开发 APK，并运行 Android 会话、共享业务和 NSH 测试。

版本标签发布流程会额外构建关闭调试的正式 APK，使用独立 Android keystore 签名，与桌面安装包一起上传并纳入 `SHA256SUMS`；任一平台失败都不会发布。配置与本地发布命令见 [Android 发布说明](RELEASING.md#android-apk-releases)。它不读取或替换桌面更新签名密钥。已有开发包与正式包签名不同，首次安装正式包前须卸载开发包，应用数据随卸载删除；后续正式版本保留同一签名即可覆盖升级。

Android 集成测试要求已经启动的**专用临时模拟器**，默认 `emulator-5580`，可用 `NATIVE_ANDROID_SERIAL` 指定。脚本拒绝真实设备，复核 QEMU 标记，只在模拟器安装测试 APK、重置独立的 `io.github.gopurr.nativeapp` 开发包，并为该测试包预授权 VPN。它不启动或清理用户已有 AVD。测试结果保存在 `bin/native/android/test/last-result.json`；本地 HTTP 测试源不使用真实订阅或公网服务。

测试使用独立的 loopback ADB 服务（默认端口 5039，`GOPURR_TEST_ADB_PORT` 可调整），禁用无线自动连接，避免默认调试服务或 IDE 的会话干扰测试。不会重启默认 5037 服务。

## 当前功能与边界

- 手机四入口为连接、节点、订阅、设置：URL 与粘贴 YAML 导入、配置启用、来源/名称编辑、更新及删除确认，规则/全局/直连、策略组与节点选择、单节点/本组测速、流量状态、诊断日志、外观与中英文。运行中的订阅更新只保存新内容并提示重新连接，保留当前 VPN 数据面。
- 未连接也能查看与搜索当前配置的节点、策略组和下次连接选择；只读已保存 YAML、内联提供器及可用本地提供器缓存，不启动内核、下载提供器或显示旧延迟。缺失缓存或需要动态名称表达式的提供器明确提示连接后加载完整列表；切换节点与测速仍要求运行中的内核。
- Android 在系统 VPN 授权后，由前台 `VpnService` 创建 IPv4/IPv6 TUN；核心使用其复制的文件描述符。路由与 DNS 由原生宿主管理，核心出站使用 `VpnService.protect()`；本应用被排除出 VPN，避免 UI 下载和核心连接进入自身隧道。
- 断开、系统撤销与服务销毁先停止核心，再释放系统 TUN。关闭 Activity 不会主动结束正在运行的 VPN。暂未启用开机连接或 Always-on VPN。
- Android 连接状态通过 `android.session` 快照与事件提供，等待系统授权时可取消；旧任务按代次和网卡所有权清理，不会关闭新连接。网卡地址、路由、MTU 和 DNS 来自 Go 生成的版本化 TunnelPlan，接入前校验 token。
- 手选节点按订阅分别持久化，路由模式也会在进程重启后恢复；重开应用仍须明确连接。断开/重连及 VPN 启动恢复该订阅的选择，新配置移除成员时回默认并清理旧选择，删除订阅会清理记忆。保存失败时保留原模式或恢复原节点。
- Android 提供系统文件选择导入、可选通知权限，以及所有应用/仅选中/排除选中的 VPN 应用范围。仅选中列表为空时拒绝保存/接入，Gopurr 自身始终排除；这不是为各应用分配代理组的功能。
- Android 使用自适应启动器图标，猫形前景按安全区域放大；VPN 常驻通知可选择实时上下行流量或仅连接状态，两行速率分别附带累计流量，断开按钮放在右侧同行，正文点击返回连接页。设置页提供通知授权和系统通知设置入口。亮屏每 2 秒更新，省电模式 10 秒；熄屏暂停速率更新，亮屏立即恢复。
- Android 正式签名版本在设置页提供应用内更新：从固定的 GitHub Releases 地址读取 `update-android.json`，按当前 ABI 下载 APK，校验 SHA-256、应用 ID、版本号及当前签名证书后，通过系统 `PackageInstaller` 打开安装确认。调试包不执行更新；通知权限与“安装未知应用”授权分别引导。
- Mac Catalyst 使用完整八页桌面：概览、订阅、代理、连接、组网、规则、日志、设置；支持原业务的聚合、节点/策略组编辑、应用分流、YAML、覆写库、DNS/嗅探、备份、诊断、远程 Web 与快捷键。系统代理和 TUN 继续调用原桌面恢复/授权协议。
- 桌面支持 320ms 单套导航几何过渡（由 NSH 原生 `LayoutAnimation` 驱动，一次提交完成）、400ms 真实流量历史插值与光轨，遵循系统减少动态效果；侧栏切换和切页保留筛选/草稿。
- iOS Simulator 保留手机入口，尚未接入 iOS Packet Tunnel Extension。Android 的四页入口暂不包含新增桌面编辑器。
- 独立预览的整包更新保持禁用。正式包保留原应用身份及更新公钥，旧版与 NSH 清单指向同一整包；Swift 窗口退出后后台再结束，由安装器完成替换并启动新宿主。发布流程会验证生产签名包，并用临时密钥和实际 NSH 宿主验收连续两次更新。管理员授权与真实外部登录仍由用户主动执行。

Android 已完成一次 OPPO 真机拔线熄屏 VPN 待机 30 分钟观测，数据和限制见 [性能记录](PERFORMANCE.md)。过夜运行、重复耗电对照、Wi-Fi/蜂窝切换与厂商后台限制仍需真机验收；模拟器与本地流量测试不能替代这些验证。

手机性能与能耗的已实现保障、剩余开销及真机对照验收见 [性能与能耗](PERFORMANCE.md)。转用 NSH 不代表耗电已经达标；界面后台停止周期刷新，VPN 数据面仍独立运行。

Android 的功能取舍、共享业务层边界和分阶段验收见 [Android 特化与架构方案](ANDROID_PLAN.md)。该文档是调研方案，尚未完成的功能不包含在上述当前能力中。

## 数据与信任链

Android 使用应用私有目录 `files/gopurr`；Apple 独立预览使用 `Application Support/Gopurr Native`。macOS 正式包沿用旧桌面的 `Application Support/Gopurr` 和原 `prefs.json` schema，不改变订阅来源路径、缓存、恢复记录或组网身份的位置。首次启动前将订阅、凭据、覆写、偏好、远程控制及组网状态原样保存到私有 `migration-nsh-v1/`，成功后原子提交清单；失败保留数据并阻止启动，重启不会覆盖初始快照。测试可指定 `GOPURR_NATIVE_HOME` 或 `--gopurr-home`；独立预览数据不会自动合并进正式目录。

手机引擎每个进程只允许一个实例，且进程不能换用另一个数据目录，因为上游缓存具有进程级单例。关闭后可以使用相同目录重新打开。

手机路由模式和按订阅保存的手选节点位于私有 `mobile-runtime.json`（schema 1），原子替换且权限为 0600；旧版本没有此文件时沿用默认值。读取时拒绝链接、损坏或不兼容版本，保留原文件并明确报错，避免悄悄改变路由行为。此文件不读取桌面偏好，也不会触发自动连接。

手机源配置中的控制接口、额外入站、进程扫描、系统路由和后台地理数据更新由引擎剥离或覆盖。桌面保留原版完整配置能力和安全校验。对外只提供有版本号的窄接口；订阅摘要不返回 URL 凭据，失败导入或启用保留原配置。

现有 `mygo.json` 更新公钥和 GitHub Secret `MYGO_UPDATER_PRIVATE_KEY` 保持原样。macOS 正式发布生成 `update-darwin-arm64.json` 与 `update-native-darwin-arm64.json`，两者指向同一签名整包。旧版升级与新客户端后续更新均走整包验签和安装流程；NSH 模块签名不替代它。尚未发布的新改动需使用高于已发布版本的新 tag，不能覆盖既有正式版本。

## 原生接口

```c
char *GopurrOpen(char *options_json);       // {home, mixedPort?} -> {handle} / {error}
char *GopurrCall(uint64_t handle, char *request_json);
char *GopurrStartVPN(uint64_t handle, int fd, void *protect_callback);
char *GopurrStartVPNWithPlan(uint64_t handle, int fd, void *protect_callback, char *token);
char *GopurrStopVPN(uint64_t handle);
void GopurrClose(uint64_t handle);
void GopurrCancelPending(uint64_t handle);  // 撤权/断开前取消在途下载和测速
void GopurrFree(char *response_json);
```

每个返回的字符串（包括错误）都须调用 `GopurrFree`。VPN callback 形如 `int protect(int fd)`，可能在多个 Go 原生线程被调用，直到停止或关闭操作返回前必须有效。Go 复制 VPN 描述符，原描述符由 Android 服务持有。

`GopurrCall` 接受 `{id?, method, params?}`，返回 `{id?, result}` 或 `{id?, error:{code,message}}`。当前接口版本为 1，方法及字段的类型定义见 `sdk/engine/types.go`：

| 方法 | 参数 |
| --- | --- |
| `engine.status` / `engine.start` / `engine.stop` | 无 |
| `engine.traffic` | 无；仅上下行速率、累计字节和时间，不采样内存/连接列表 |
| `engine.mode` | `{mode:"rule"\|"global"\|"direct"}` |
| `profiles.list` | 无 |
| `profiles.import` | `{name,url}` 或 `{name,yaml}` |
| `profiles.activate` | `{id}` |
| `profiles.editor` | `{id}`，仅显式编辑请求返回完整来源 URL |
| `profiles.update` | `{id,name?,url?}`，修改来源后需明确更新才下载 |
| `profiles.refresh` / `profiles.delete` | `{id}` |
| `proxies.groups` | 无 |
| `proxies.select` | `{group,member}` |
| `proxies.delay` | `{name,url?}` |
| `logs.list` | 无 |

Android 宿主拦截 `engine.start` / `engine.stop` 来执行系统授权与 VPN 生命周期；其他请求使用同一 JSON 协议。VPN 接通期间禁止激活其他配置，先断开后切换。

Android 手机界面优先使用独立宿主方法 `android.connect` / `android.disconnect`（立即返回会话快照），通过 `android.session` 同名事件观察进度。`android.apps`、`android.scope.get/set`、`android.file.pick` 和 `android.notifications` / `android.notifications.request` 和 `android.appearance` 处理平台能力；状态栏与导航栏图标跟随实际外观。业务协议仍为 v1，新增状态 `busy`、`operation`、`reconnectRequired`；`profiles.import` 保留直接返回订阅摘要，新 mutation 返回保存/应用结果与状态。普通 JSON 调用不能传入 VPN FD。

Android 应用更新使用独立宿主方法 `android.update`、`android.update.check`、`android.update.download`、`android.update.cancel` 和 `android.update.install`。更新清单只接受 HTTPS GitHub Releases 地址，APK 必须与当前安装包同包名、同版本号且同一签名证书；下载文件放在应用私有缓存，安装由系统确认，不能静默覆盖。

## 界面验收

`native/ui/Preview.tsx` 是独立演示入口，有可见演示标签。真实 `App.tsx` 只调用原生宿主，桥接缺失或出错时显示错误，不会切换到假数据。

桌面八页明暗截图覆盖 1060×720、840×560，主要编辑器、错误状态和动画定格也有实际 AppKit 截图；脚本为 `desktop-render.steps.js`、`desktop-editors.steps.js`、`desktop-dialogs.steps.js`。手机四页明暗截图覆盖 1060×720、840×560、390×844。它们位于 `native/ui/screenshots/`；生成流程见 `native/ui/render.steps.js`。截图需结合 NSH 交互测试与实际 Android 宿主验证。

`go test -race` 仍能报告 mihomo 上游日志级别全局变量的竞争：配置应用写入 `log.level`，同时旧代理的 GC finalizer 读取它。引擎已在加载前停止旧连接，消除了此前入站 ACL 的重载竞争；剩余日志问题及上游位置记录在 `sdk/engine/README.md`。功能测试通过不代表竞争检查已全部通过。

## Windows 正式桌面

Windows 10 1809 及以上 x64 使用 `Gopurr.exe` NSH 宿主、`GopurrBridge.dll` 与 `gopurr-desktop.exe` 后台。正式构建默认选择 NSH，并在发布前验收生产更新签名、实际宿主连续升级、旧数据、安装/卸载、核心流量、Wintun 和系统服务。

原 `%APPDATA%\Gopurr` 数据原位保留，首次接管保存私有 `migration-nsh-v1` 快照。`update-windows-amd64.json` 与 `update-native-windows-amd64.json` 指向同一签名整包；包内同时提供新卸载器。现有正式版可直接应用内更新，程序身份、安装路径、协议和快捷方式沿用原值。

```powershell
go run ./scripts/release -platform windows/amd64
# 正式签名由 MYGO_UPDATER_PRIVATE_KEY 显式提供：
go run ./scripts/release -platform windows/amd64 -release
```

单独构建宿主可运行 `scripts/build-native-windows.ps1`，随后用 `scripts/package-native-windows.ps1` 生成完整安装与更新负载。`-native-windows=false` 只保留本地旧桌面对照构建，正式发布拒绝选择旧桌面。
