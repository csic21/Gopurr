import { cleanup, effect, lifecycle, Scroll, signal, Text, View } from "nsh";
import { Input } from "nsh/base";
import { call } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { androidCall, androidHost } from "./platform";
import { message, refreshStatus } from "./store";
import { Action, Empty, Field, Heading, Label, Panel } from "./ui";

export interface MeshDevice {
  id: string;
  name: string;
  dnsName: string;
  os: string;
  owner: string;
  login: string;
  ips: string[];
  tags: string[];
  online: boolean;
  active: boolean;
  lastSeen: string;
  path: string;
}

export interface MeshService {
  protocol: string;
  port: number;
}

export interface MeshVisit {
  key: string;
  deviceId: string;
  device: string;
  owner: string;
  login: string;
  remote: string;
  local: string;
  protocol: string;
  active: boolean;
  firstSeen: string;
  lastSeen: string;
}

export interface MeshSnapshot {
  managed: boolean;
  started: boolean;
  busy: boolean;
  action: string;
  autoConnect: boolean;
  accessReady: boolean;
  error: string;
  monitorError: string;
  updated: string;
  status: {
    installed: boolean;
    official: boolean;
    running: boolean;
    shieldsKnown: boolean;
    shieldsUp: boolean;
    state: string;
    version: string;
    authURL: string;
    tailnet: string;
    self: MeshDevice;
    peers: MeshDevice[];
    services: MeshService[];
    blocked: string[];
    health: string[];
  };
  visits: MeshVisit[];
}

export interface MeshProbeResult {
  deviceId: string;
  state: string;
  message: string;
  port: number;
  checked: string;
}

function deviceIP(device?: MeshDevice): string {
  const ips = device?.ips ?? [];
  return ips.find(ip => !ip.includes(":")) ?? ips[0] ?? "";
}

function formatServices(services?: MeshService[]): string {
  return (services ?? []).map(s => `${s.port}/${s.protocol}`).join(", ");
}

function defaultProbePort(device: MeshDevice): string {
  const os = (device.os ?? "").toLowerCase();
  if (os.includes("windows")) return "3389";
  if (os.includes("mac") || os.includes("darwin")) return "5900";
  return "22";
}

function probeProtocol(port: number): string {
  if (port === 3389) return "rdp";
  if (port === 5900) return "vnc";
  return "ssh";
}

export function MeshPage(props: { onBack: () => void }) {
  const snap = signal<MeshSnapshot | null>(null);
  const loaded = signal(false);
  const working = signal(false);
  const meshError = signal("");
  const meshNotice = signal("");
  const servicesDraft = signal("");
  const servicesDirty = signal(false);
  const search = signal("");
  const probePorts = signal<Record<string, string>>({});
  const probing = signal<Record<string, boolean>>({});
  const probeResults = signal<Record<string, MeshProbeResult>>({});

  let disposed = false;
  let noticeTimer: ReturnType<typeof setTimeout> | undefined;
  let requestSeq = 0;

  cleanup(() => {
    disposed = true;
    clearTimeout(noticeTimer);
  });

  function showNotice(text: string) {
    clearTimeout(noticeTimer);
    meshNotice.set(text);
    noticeTimer = setTimeout(() => {
      if (!disposed) meshNotice.set("");
    }, 4000);
  }

  function applySnapshot(next: MeshSnapshot) {
    snap.set(next);
    loaded.set(true);
    if (!servicesDirty()) servicesDraft.set(formatServices(next.status?.services));
  }

  async function loadStatus() {
    const seq = ++requestSeq;
    const next = await call<MeshSnapshot>("mesh.status");
    if (disposed || seq !== requestSeq) return;
    applySnapshot(next);
  }

  effect(() => {
    if (lifecycle() !== "active") return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function poll() {
      try {
        if (!working()) await loadStatus();
      } catch (err) {
        if (!stopped && !disposed && !meshError()) meshError.set(message(err));
      } finally {
        if (!stopped && !disposed) {
          const st = snap()?.status;
          const fast = !!snap()?.busy || st?.state === "Starting" || st?.state === "NeedsLogin";
          timer = setTimeout(poll, fast ? 1500 : 4000);
        }
      }
    }
    void poll();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  });

  const status = () => snap()?.status;
  const busy = () => working() || !!snap()?.busy;
  const allowed = () => !!status()?.shieldsKnown && !status()?.shieldsUp;
  const blocked = () => status()?.blocked ?? [];
  const isBlocked = (id: string) => blocked().includes(id);
  const devices = () => {
    const s = status();
    return [...(s?.self?.id ? [s.self] : []), ...(s?.peers ?? [])];
  };
  const filtered = () => {
    const q = search().trim().toLowerCase();
    return devices().filter(d => !q || [d.name, d.dnsName, d.owner, d.login, d.os, ...(d.ips ?? [])].join(" ").toLowerCase().includes(q));
  };

  async function mutate(fn: () => Promise<void>, syncOverview = false) {
    if (working()) return;
    working.set(true);
    meshError.set("");
    requestSeq++;
    try {
      await fn();
      if (syncOverview) void refreshStatus().catch(() => {});
    } catch (err) {
      if (!disposed) meshError.set(message(err));
    } finally {
      if (!disposed) working.set(false);
    }
  }

  function runAction(action: "connect" | "disconnect" | "refresh" | "allow" | "block" | "shieldsUp" | "shieldsDown" | "clearHistory") {
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.action", { action });
      if (!disposed) applySnapshot(next);
    }, action !== "clearHistory");
  }

  function openLogin() {
    return mutate(async () => {
      const res = await call<{ url?: string }>("mesh.action", { action: "login" });
      const url = res?.url || status()?.authURL || "";
      if (!url) throw new Error(tr("暂无可用的登录链接，请稍后重试。", "No sign-in URL is available yet. Try again shortly."));
      if (!/^https?:\/\//i.test(url)) throw new Error(tr("登录链接格式无效。", "Invalid sign-in URL."));
      if (androidHost()) {
        try {
          await androidCall("android.url.open", { url });
          if (!disposed) showNotice(tr("已打开浏览器授权页，完成登录后状态会自动更新。", "Opened browser for sign-in. Status refreshes after authorization."));
        } catch {
          if (!disposed) showNotice(tr(`无法打开浏览器，请手动访问：${url}`, `Could not open browser. Visit: ${url}`));
        }
      } else {
        if (!disposed) showNotice(tr(`请在浏览器中打开登录链接：${url}`, `Open sign-in URL in browser: ${url}`));
      }
      await loadStatus().catch(() => {});
    });
  }

  function openServices() {
    const raw = servicesDraft().trim();
    if (!raw) return;
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.services", { services: raw, enabled: true });
      if (!disposed) {
        servicesDirty.set(false);
        applySnapshot(next);
        showNotice(tr("已开放并保存授权端口。", "Authorized ports saved and opened."));
      }
    }, true);
  }

  function toggleAutoConnect() {
    const nextEnabled = !snap()?.autoConnect;
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.autoConnect", { enabled: nextEnabled });
      if (!disposed) applySnapshot(next);
    });
  }

  function toggleBlock(id: string, nextBlocked: boolean) {
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.block", { id, blocked: nextBlocked });
      if (!disposed) {
        applySnapshot(next);
        showNotice(nextBlocked ? tr("已在本机封禁该设备。", "Device blocked on this machine.") : tr("已解除对该设备的封禁。", "Device unblocked."));
      }
    });
  }

  async function copyIP(ip: string) {
    if (!ip) return;
    if (androidHost()) {
      try {
        await androidCall("android.clipboard.write", { text: ip });
        if (!disposed) showNotice(tr(`已复制组网 IP：${ip}`, `Copied mesh IP: ${ip}`));
        return;
      } catch {
        // Fall through to notice showing the IP when clipboard bridge is unavailable.
      }
    }
    if (!disposed) showNotice(tr(`组网 IP：${ip}`, `Mesh IP: ${ip}`));
  }

  async function probeDevice(device: MeshDevice) {
    const rawPort = (probePorts()[device.id] ?? defaultProbePort(device)).trim();
    const port = Number(rawPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      meshError.set(tr("检测端口须为 1–65535 的整数。", "Probe port must be an integer between 1 and 65535."));
      return;
    }
    meshError.set("");
    probing.set({ ...probing(), [device.id]: true });
    try {
      const result = await call<MeshProbeResult>("mesh.probe", { id: device.id, protocol: probeProtocol(port), port });
      if (!disposed) probeResults.set({ ...probeResults(), [device.id]: result });
    } catch (err) {
      if (!disposed) meshError.set(message(err));
    } finally {
      if (!disposed) {
        const next = { ...probing() };
        delete next[device.id];
        probing.set(next);
      }
    }
  }

  function connectionStatusText(): string {
    const st = status();
    if (st?.running) return tr("已连接 Tailscale", "Connected to Tailscale");
    if (snap()?.busy || st?.state === "Starting") return tr("正在连接…", "Connecting…");
    if (st?.state === "NeedsLogin" || !!st?.authURL) return tr("等待浏览器授权登录", "Waiting for browser sign-in");
    if (st?.state === "NeedsMachineAuth") return tr("等待管理员审批此设备", "Waiting for machine approval");
    return loaded() ? tr("尚未连接组网", "Mesh disconnected") : tr("正在读取组网状态…", "Loading mesh status…");
  }

  function connectionStatusColor(): string {
    const st = status();
    if (st?.running) return colors().success;
    if (snap()?.busy || st?.state === "Starting" || st?.state === "NeedsLogin" || !!st?.authURL) return colors().accent;
    return colors().muted;
  }

  function goBack() {
    void refreshStatus().catch(() => {});
    props.onBack();
  }

  const combinedError = () => meshError() || snap()?.error || snap()?.monitorError || "";
  const servicesText = () => formatServices(status()?.services);
  const selfIdentity = () => [deviceIP(status()?.self), status()?.tailnet].filter(Boolean).join(" · ");

  return <Scroll testID="mesh-scroll" style={{ flex: 1 }} contentStyle={{ padding: 16, gap: 16 }}>
    <Heading
      title={tr("组网", "Mesh")}
      description={tr("通过 Tailscale 连接自己的设备，切到直连或不开代理均不影响组网。", "Connect your devices via Tailscale. Works in Direct mode or without a proxy.")}
      action={() => <Action testID="mesh-back" label={tr("返回", "Back")} disabled={false} onPress={goBack} />}
    />

    <If when={!!combinedError()}>
      <View testID="mesh-error" role="alert" style={{ padding: 12, borderRadius: 8, borderWidth: 1, borderColor: colors().danger, backgroundColor: colors().surface, gap: 8 }}>
        <Text style={{ color: colors().danger, fontSize: 12, lineHeight: 18 }}>{combinedError()}</Text>
        <If when={!!meshError()}>
          <View style={{ flexDirection: "row", gap: 8 }}>
            <Action label={tr("关闭提示", "Dismiss")} disabled={false} onPress={() => meshError.set("")} />
          </View>
        </If>
      </View>
    </If>

    <If when={!!meshNotice()}>
      <View testID="mesh-notice" style={{ padding: 12, borderRadius: 8, backgroundColor: colors().selected }}>
        <Text style={{ color: colors().text, fontSize: 12, lineHeight: 18 }}>{meshNotice()}</Text>
      </View>
    </If>

    <Panel>
      <View style={{ gap: 6 }}>
        <Text style={{ color: connectionStatusColor(), fontSize: 13, fontWeight: "600" }}>{connectionStatusText()}</Text>
        <If when={!!status()?.self?.name}>
          <Text style={{ fontSize: 18, fontWeight: "600", color: colors().text, numberOfLines: 2 }}>{status()!.self.name}</Text>
        </If>
        <If when={!!selfIdentity()}>
          <Text style={{ fontSize: 12, color: colors().muted, fontVariant: "tabular-nums" }}>{selfIdentity()}</Text>
        </If>
        <Label text={tr("直连模式或未导入代理订阅时，组网路由仍可正常使用。", "Mesh routing keeps working in Direct mode or without a proxy profile.")} muted size={12} />
        <If when={!!status()?.running}>
          <Label text={snap()?.accessReady ? tr("设备路由已就绪，可直接互通组网设备。", "Device routing is ready for mesh peers.") : tr("可在概览页开启 VPN 以接管设备组网流量。", "Start VPN from Connect to route device traffic to mesh peers.")} muted size={12} />
        </If>
        <If when={(status()?.health ?? []).length > 0}>
          <Label text={(status()?.health ?? []).join(" · ")} muted size={11} />
        </If>
      </View>
      <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
        <If when={!status()?.running && !snap()?.busy && !status()?.authURL}>
          <Action testID="mesh-connect" primary label={tr("连接组网", "Connect mesh")} disabled={working()} onPress={() => void runAction("connect")} />
        </If>
        <If when={!!status()?.authURL}>
          <Action testID="mesh-login" primary={!status()?.running} label={tr("浏览器登录", "Sign in via browser")} disabled={working()} onPress={() => void openLogin()} />
        </If>
        <If when={!!status()?.running || !!snap()?.busy || !!status()?.authURL}>
          <Action testID="mesh-disconnect" label={status()?.running ? tr("断开组网", "Disconnect mesh") : tr("取消连接", "Cancel")} disabled={working()} onPress={() => void runAction("disconnect")} />
        </If>
        <Action testID="mesh-refresh" label={tr("刷新", "Refresh")} disabled={working()} onPress={() => void runAction("refresh")} />
      </View>
      <View style={{ gap: 6, paddingTop: 4 }}>
        <Action testID="mesh-autoconnect" label={snap()?.autoConnect ? tr("启动时自动连接：已开启", "Auto-connect: On") : tr("启动时自动连接：已关闭", "Auto-connect: Off")} disabled={busy()} onPress={() => void toggleAutoConnect()} />
        <Label text={tr("使用已保存授权在启动时重连，不自动开放入站端口。", "Reconnect on startup using saved credentials; inbound ports remain closed.")} muted size={11} />
      </View>
    </Panel>

    <Panel>
      <Label text={tr("入站服务授权", "Inbound service authorization")} bold size={16} />
      <Text style={{ fontSize: 13, fontWeight: "600", color: allowed() ? colors().success : colors().muted }}>
        {allowed()
          ? (servicesText() ? tr(`已开放 · ${servicesText()}`, `Open · ${servicesText()}`) : tr("已开放", "Open"))
          : tr("入站访问已关闭", "Inbound access closed")}
      </Text>
      <Label text={tr("授权只放行指定端口的组网流量，不会自动启动对应服务；仍可主动访问其他设备。", "Authorization permits mesh traffic on explicit ports only and does not start services; outbound access remains available.")} muted size={12} />
      <Field label={tr("授权端口", "Authorized ports")} testID="mesh-services-input" value={servicesDraft()} onChange={value => { servicesDirty.set(true); servicesDraft.set(value); }} placeholder="8080/tcp, 5555/tcp" disabled={busy()} />
      <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
        <Action testID="mesh-services-open" label={tr("开放并保存端口", "Open & save ports")} disabled={busy() || !servicesDraft().trim()} onPress={() => void openServices()} />
        <Action testID="mesh-services-block" danger={allowed()} label={tr("关闭入站访问", "Close inbound access")} disabled={busy() || (!!status()?.shieldsKnown && !!status()?.shieldsUp)} onPress={() => void runAction("block")} />
      </View>
    </Panel>

    <Panel>
      <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
        <Label text={tr("组网设备", "Devices")} bold size={16} />
        <Label text={tr(`${filtered().length} 台设备`, `${filtered().length} devices`)} muted size={12} />
      </View>
      <Field label={tr("搜索设备", "Search devices")} testID="mesh-device-search" value={search()} onChange={value => search.set(value)} placeholder={tr("按名称、账号、系统或 IP 搜索", "Search by name, owner, OS, or IP")} />
      <If when={!filtered().length}>
        <Empty
          title={!status()?.running && !devices().length
            ? tr("连接后显示设备", "Devices appear once connected")
            : !devices().length
              ? tr("暂无可显示的设备", "No devices to display")
              : tr("没有匹配的设备", "No matching devices")}
          description={!status()?.running && !devices().length
            ? tr("连接组网后，同一网络中的设备会显示在这里。", "Devices in the same tailnet appear here once connected.")
            : !devices().length
              ? tr("同一网络中有权限查看的设备会显示在这里。", "Authorized devices in your tailnet appear here.")
              : tr("换个关键词，或清除搜索条件。", "Try another keyword or reset the filter.")}
          action={() => <If when={!!search().trim()}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={() => search.set("")} /></If>}
        />
      </If>
      <Each of={filtered()} key={d => d.id}>
        {d => {
          const isSelf = () => d().id === status()?.self?.id;
          const ip = () => deviceIP(d());
          const blockedDevice = () => isBlocked(d().id);
          const probe = () => probeResults()[d().id];
          return <View testID={`mesh-device-${d().id}`} style={{ padding: 12, borderRadius: 10, borderWidth: 1, borderColor: colors().border, backgroundColor: colors().bg, gap: 10 }}>
            <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
              <Text style={{ flex: 1, fontSize: 14, fontWeight: "600", color: colors().text, numberOfLines: 2 }}>
                {d().name + (isSelf() ? tr(" · 本机", " · This device") : "")}
              </Text>
              <View style={{ paddingHorizontal: 8, paddingVertical: 3, borderRadius: 999, backgroundColor: colors().surface, borderWidth: 1, borderColor: blockedDevice() ? colors().danger : d().online ? colors().success : colors().border }}>
                <Text style={{ fontSize: 11, fontWeight: "600", color: blockedDevice() ? colors().danger : d().online ? colors().success : colors().muted }}>
                  {blockedDevice() ? tr("已封禁", "Blocked") : d().online ? tr("在线", "Online") : tr("离线", "Offline")}
                </Text>
              </View>
            </View>
            <View style={{ gap: 2 }}>
              <If when={!!(d().owner || d().login || d().os)}>
                <Label text={[d().owner || d().login, d().os].filter(Boolean).join(" · ")} muted size={12} />
              </If>
              <Text style={{ fontSize: 12, color: colors().muted, fontVariant: "tabular-nums" }}>{ip() || "—"}</Text>
            </View>
            <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap", alignItems: "center" }}>
              <Action testID={`mesh-copy-${d().id}`} label={tr("复制 IP", "Copy IP")} disabled={!ip()} onPress={() => void copyIP(ip())} />
              <If when={!isSelf()}>
                <Input
                  testID={`mesh-probe-port-${d().id}`}
                  value={probePorts()[d().id] ?? defaultProbePort(d())}
                  onChange={({ value }) => probePorts.set({ ...probePorts(), [d().id]: value })}
                  placeholder="22"
                  keyboardType="number-pad"
                  autoCapitalize="none"
                  autoCorrect={false}
                  autoComplete="off"
                  accessibilityLabel={tr("检测端口", "Probe port")}
                  style={{ width: 84, minHeight: 44, paddingHorizontal: 10, paddingVertical: 8, borderWidth: 1, borderColor: colors().border, borderRadius: 8, color: colors().text, fontSize: 14, fontVariant: "tabular-nums", backgroundColor: colors().surface, placeholderColor: colors().faint }}
                />
                <Action testID={`mesh-probe-${d().id}`} label={probing()[d().id] ? tr("检测中…", "Probing…") : tr("检测端口", "Probe")} disabled={!!probing()[d().id] || !status()?.running || !d().online || blockedDevice()} onPress={() => void probeDevice(d())} />
                <Action testID={`mesh-block-${d().id}`} danger={!blockedDevice()} label={blockedDevice() ? tr("解除封禁", "Unblock") : tr("封禁设备", "Block")} disabled={busy()} onPress={() => void toggleBlock(d().id, !blockedDevice())} />
              </If>
            </View>
            <If when={!!probe()}>
              <Text testID={`mesh-probe-result-${d().id}`} style={{ fontSize: 12, lineHeight: 18, color: probe()?.state === "connected" || probe()?.state === "verified" ? colors().success : probe()?.state === "failed" ? colors().danger : colors().muted }}>
                {`${probe()?.port ? `${probe()?.port}: ` : ""}${probe()?.message || probe()?.state || ""}`}
              </Text>
            </If>
          </View>;
        }}
      </Each>
    </Panel>

    <If when={(snap()?.visits ?? []).length > 0}>
      <Panel>
        <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
          <Label text={tr("访问记录", "Access history")} bold size={16} />
          <Action testID="mesh-clear-history" label={tr("清空记录", "Clear history")} disabled={busy()} onPress={() => void runAction("clearHistory")} />
        </View>
        <Each of={snap()?.visits ?? []} key={v => `${v.key}-${v.firstSeen}`}>
          {v => <View style={{ paddingVertical: 6, borderTopWidth: 1, borderColor: colors().border, gap: 2 }}>
            <Label text={v().device || v().remote || tr("未知设备", "Unknown device")} bold size={13} />
            <Label text={[v().owner || v().login, v().protocol, `${v().remote} -> ${v().local}`].filter(Boolean).join(" · ")} muted size={11} />
          </View>}
        </Each>
      </Panel>
    </If>
  </Scroll>;
}
