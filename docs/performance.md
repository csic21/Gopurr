# 性能测量与第一轮优化

2026-11-03，macOS / Apple M4 / arm64，Go 1.27.3，启用 `with_gvisor`。使用合成规则和连接数据，未使用真实订阅或修改系统代理。数据为单机短时微基准的近似值，不代表整机能耗、常驻内存或竞品对比结果。MB / KB 使用十进制单位。

## 本机结果

| 操作 | 优化前 | 优化后 | 测量范围 |
| --- | --- | --- | --- |
| 10 万规则，配置未变，跨采样周期读取概览 DNS／嗅探状态 | 约 298 ms、373.6 MB 分配 | 约 0.0004 ms、768 B 分配 | 优化前缓存到期重新合成和解析 YAML；优化后仍命中相同配置版本 |
| 1 万连接，仅获取数量 | 约 10.9 ms、22.6 MB 分配 | 约 0.0012 ms、2.2 KB 分配 | 控制器客户端请求及 JSON 解析；不含服务端扫描、编码或操作系统传输 |
| 1 万连接，相同快照、相同关键词的重复筛选 | 约 8.6 ms、6.9 MB 分配 | 约 9 ns、0 B 分配 | 优化后直接复用已筛选结果；初次建立搜索索引及关键词变化另计 |
| macOS 完整系统代理状态读取 | 约 128.6 ms、215 KB Go 分配 | 约 0.57 ms、12.4 KB Go 分配 | 优化前为 1 次 route 和 7 次 networksetup；优化后为只读系统框架调用；不含子进程／原生框架的内存 |

概览的首次配置编译仍需处理完整 YAML，现于后台完成。规则数或连接数本身未减少，原生界面的采样频率也没有用来替代故障保护。系统代理健康检查保持原来的频率与恢复策略，节省的是每次观察启动子进程的工作。

## 缓存及并发边界

- 概览签名包括全局覆写、本地配置、活动订阅、存储实例与提交版本。缓存有效时每秒最多检查一次订阅文件元数据；显式刷新和来源变更立即检查。后台编译结果发布前重新检查来源，过期结果丢弃；编辑器和开关操作仍读取当前配置。
- 桌面与远程 Web 的连接列表共享最多两秒的不可变快照。并发读者共用一次请求，单个读者取消不会中断其他读者；断开连接或控制器重连使快照失效，跨失效的旧响应不能发布。
- 新 `/gopurr/connections/count` 接口仅返回数量，受现有控制器令牌保护。内核仍需遍历连接，工作量是 O(N)，但不再生成或传输完整连接详情。旧服务返回 403／404 时回退到原接口，不将认证失败或其他服务错误当成缺少能力。
- 节点切换串行执行，窗口连续操作只保留最后一项等待请求。旧连接清理最多同时执行 8 项，总预算为 10 秒，范围限定为操作前快照中经过该策略组的连接；不会对其他策略组执行全量断开。超时取消进行中的请求并报告未清理数量，避免失去响应的内核长时间阻塞后续切换或退出。
- 配置指纹描述内核实际接受的完整应用 payload，不能仅凭磁盘文件推断。只有指纹相同且本地代理／TUN 状态验证通过才省略重载；部分配置修改会清除指纹，显式手动重载始终执行。旧服务不需要强制升级，缺少指纹时保持原有重载行为。
- macOS 原生观察保留未启用协议的服务器及端口，同时读取 PAC、自动发现、绕过规则与网络服务名称。系统框架不可加载时回退到原命令路径；系统状态读取错误仍向调用方报告。

## 复现

使用 Makefile 的仓库内缓存：

```sh
export GOPATH="$PWD/.cache/go" GOMODCACHE="$PWD/.cache/mod" GOCACHE="$PWD/.cache/build"
export GOTOOLCHAIN=go1.27.3 GOFLAGS=-tags=with_gvisor
rtk go test ./internal/app ./internal/kernel -run '^$' \
  -bench 'Benchmark(OverviewFeaturesUnchanged|ConnectionSearchUnchanged10K|ConnectionCount)$' \
  -benchtime=250ms -benchmem -count=1
```

macOS 的以下检查只读当前机器的代理设置，比较原生 API 与原命令路径的完整字段。它默认不参与普通测试，不进行授权或设置写入：

```sh
GOPURR_TEST_PROXY_OBSERVE=1 rtk go test ./internal/sysproxy \
  -run '^TestNativeProxyObserver' \
  -bench 'Benchmark(Native|Command)ProxyObservation$' \
  -benchtime=250ms -benchmem -count=1
```

普通回归和 UI 渲染使用临时本地 HTTP 服务、Unix socket 和假系统代理。新增并发检测可用：

```sh
rtk go test -race ./internal/kernel -count=1
rtk go test -race ./internal/app \
  -run 'Test(FeatureCacheTracksProfileRefreshOverridesAndExternalEdits|OverviewFeatureComputationRejectsStaleSource|ConnectionPageDoesNotWaitForController|ProxyCleanupHasBoundedConcurrencyAndKeepsOtherGroups|ProxySelectionIncludesRadioWhitespaceAndLatency)$' \
  -count=1
```

后续整机对照应使用相同平台、内核版本、配置与网络负载，分别记录前台、关窗、轻量模式和远程 Web 活跃时的 CPU、进程树内存、唤醒次数及能耗；用长连接验证配置变更期间的连接存活率。当前微基准不能证明优于 Clash Verge Rev 或 FlClash。
