# macOS NSH 迁移与验收

日期：2026-11-08。以下描述当前工作区及本地隔离验收，不表示已上传或发布新的 GitHub 正式版本。

## 正式入口

`make bundle` 和 `scripts/release -platform darwin/arm64` 使用 NSH / Mac Catalyst 八页窗口，后台沿用现有菜单栏、快捷键、独立内核、特权服务和恢复事务。Windows 发布入口保持原桌面。

正式应用为 `bin/darwin-arm64/Gopurr.app`，保留 `io.github.gopurr.app`、`Contents/MacOS/Gopurr` 和 macOS 12 部署下限。独立预览 `make native-bundle` 仍生成 `bin/native/Gopurr Native.app`，使用独立身份与数据目录。

## 旧数据接管

正式包继续使用 `~/Library/Application Support/Gopurr/`，保持原始 schema、订阅凭据与文件来源路径；提供器和内核缓存也留在原处。避免搬动目录造成绝对路径、恢复记录或组网身份失效。默认目录使用与旧版相同的 mygo 单实例锁，原生后台另有按目录的 OS 文件锁。

首次加载模型之前，将下列持久状态原样备份到 `migration-nsh-v1/`：

- `prefs.json`、`overrides.json`（包含规则与模板库）、`local.yaml`。
- `profiles.json` 和 `profiles/` 中的订阅与凭据。
- `remote.json`、`mesh.json`、`tailnet/` 中的访问设置与组网身份。
- `sysproxy.json`、`sysproxy-recovery.json` 恢复记录。

目录权限为 0700、文件为 0600；清单记录来源目录和每个文件的 SHA-256。所有文件写入完成后原子提交整个快照。JSON 损坏、符号链接或非普通文件会使首次接管失败，保持原数据并允许修复后重试。重启不会覆盖初始快照。运行 socket、日志和临时文件不进入快照。

正式目录不会自动合并独立预览目录，避免覆盖两个版本各自的订阅和偏好。`GOPURR_NATIVE_HOME`、`GOPURR_HOME` 与 `--gopurr-home` 可显式选择隔离目录，更新重启通过参数保留所选目录。

## 更新链路

保留 `mygo.json` 的公钥和既有 `MYGO_UPDATER_PRIVATE_KEY`。正式包输出完整 `.app` 的签名归档，同时输出内容完全相同的 `update-darwin-arm64.json` 与 `update-native-darwin-arm64.json`；旧客户端和 NSH 客户端因此获取同一归档。

Swift 宿主先退出，Go 后台等待宿主消失后再结束；mygo 安装器随后替换完整应用并通过 LaunchServices 启动新宿主。后台 socket、认证文件与旧 PID 参数不会传给新宿主。仅显式的数据目录参数保留。暂缓重启时继续运行当前版本，普通退出也允许已准备的更新完成替换。

正式构建器验证应用身份、数据位置、版本、原公钥、更新入口、桌面字节码及完整代码签名，拒绝把旧界面、手机界面或独立预览当成迁移包。应用包每次在空的临时目录内构建，成功后替换构建产物，避免遗留旧可执行文件和资源。

## 本地验证

已通过：

- 完整 Go 回归：852 项测试通过。
- NSH 类型检查与 40 项交互测试通过。
- 真实宿主连续升级：旧版更新器测试应用 `0.0.0 → NSH 0.0.1 → NSH 0.0.2`，最终退出竞争保护版本连续三轮通过完整下载、验签、替换与重新启动。
- 更新过程保留活跃订阅、来源凭据、节点文件、外观和语言；初始迁移快照在第二次升级后保持不变。
- 错误签名被拒绝，已安装宿主版本保持不变，随后可重试成功。
- 正式身份应用包及其完整签名归档通过校验；本地 DMG 完整性验证通过，并只读挂载检查包内应用身份、代码签名、版本和 Applications 安装链接。
- 正式后台的真实内核、本地代理流量、八页、无效配置回滚、备份和正常退出验收通过，未请求系统网络变更。
- 迁移失败后没有半成品快照，修复数据后可重试；正式包双清单与旧归档命名验证通过。
- 八页明暗截图重新渲染，覆盖 1060×720 与 840×560；最小窗口系统代理、TUN、DNS 和嗅探控制可见。

完整升级测试使用临时生成的 Ed25519 密钥、本地 HTTP、唯一测试应用身份和合成数据目录；首次更新器使用与旧版一致的真实 mygo 更新模块，后两次运行实际 Swift / NSH 窗口与生产 Go 后台。它不会操作用户已安装应用、真实订阅或系统代理，也不会读取正式私钥。

```sh
GOPATH="$PWD/.cache/go" GOMODCACHE="$PWD/.cache/mod" \
GOCACHE="$PWD/.cache/build" GOTOOLCHAIN=go1.27.4 GOFLAGS=-tags=with_gvisor \
GOPURR_NATIVE_PACKAGE_TEST_APP="$PWD/bin/darwin-arm64/Gopurr.app" \
go test -v ./scripts/package-native-update \
  -run '^TestNativeDesktopMigrationRoundtrip$' -count=1 -timeout=12m
```

Release 工作流在发布前运行此测试，并用原生产公钥通过 `scripts/verify-update` 安装当次正式签名归档；任一检查失败即不发布。新的线上切换必须使用高于既有正式版的新 tag，现有公开版本不会被覆盖。本地测试没有使用生产私钥完成签名或运行远程发布任务。

Apple Developer ID / 公证仍沿用项目现状，未由本次迁移新增；Ad hoc 代码签名校验与 Ed25519 更新验签均不能替代 Apple 公证。macOS 12 的部署目标已检查，本轮运行环境不是 macOS 12 实机；真实管理员授权、外部账号登录仍需按对应平台操作验收。
