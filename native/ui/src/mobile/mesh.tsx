import { cleanup, effect, lifecycle, List, Scroll, signal, Text, View } from "nsh";
import { Button, Input } from "nsh/base";
import { call } from "../client";
import { tr } from "../i18n";
import { colors } from "../theme";
import { androidCall, androidHost, haptic } from "./platform";
import {
  awaitingMeshPermission,
  connected,
  meshTunnel,
  message,
  refreshStatus,
  session,
  showToast,
  status as engineStatus,
} from "./store";
import {
  Action,
  BottomSheet,
  confirmDialog,
  Empty,
  Field,
  Group,
  GroupRow,
  Icon,
  IconButton,
  Label,
  Notice,
  Panel,
  RADIUS,
  SearchField,
  SectionLabel,
  Segmented,
  StatusDot,
  StatusPill,
  SubpageHeader,
  Tag,
  tint,
  ToggleSwitch,
} from "./ui";
import type { Tone } from "./ui";

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

type MeshTab = "devices" | "sharing" | "settings";

const PROBE_PRESETS = [
  { port: "22", label: "SSH · 22" },
  { port: "3389", label: "RDP · 3389" },
  { port: "5900", label: "VNC · 5900" },
  { port: "8080", label: "HTTP · 8080" },
];

const INBOUND_PRESETS = ["8080/tcp", "5555/tcp", "22/tcp", "3389/tcp", "5900/tcp"];

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

function formatPathShort(path?: string): string {
  const p = (path ?? "").trim();
  if (!p) return "";
  if (p.toLowerCase() === "direct") return tr("直连", "Direct");
  if (p.toLowerCase().startsWith("derp")) return p.toUpperCase().replace("DERP(", "DERP (");
  return p;
}

function formatTime(iso?: string): string {
  if (!iso) return "—";
  const ts = Date.parse(iso);
  if (Number.isNaN(ts)) return iso;
  const d = new Date(ts);
  if (d.getUTCFullYear() <= 1970) return "—";
  const mm = String(d.getMonth() + 1).padStart(2, "0");
  const dd = String(d.getDate()).padStart(2, "0");
  const hh = String(d.getHours()).padStart(2, "0");
  const min = String(d.getMinutes()).padStart(2, "0");
  return `${mm}-${dd} ${hh}:${min}`;
}

function appendServicePreset(current: string, preset: string): string {
  const existing = current
    .split(/[\s,，]+/)
    .map(v => v.trim())
    .filter(Boolean);
  if (existing.some(v => v.toLowerCase() === preset.toLowerCase())) return existing.join(", ");
  return [...existing, preset].join(", ");
}

export function MeshGlanceCard(props: { onOpen: () => void }) {
  const mesh = () => engineStatus()?.mesh;

  const tone = (): Tone => {
    const m = mesh();
    if (awaitingMeshPermission()) return "warn";
    if (m?.error && !m?.running) return "danger";
    if (m?.state === "NeedsLogin" || !!m?.authURL) return "accent";
    if (m?.state === "NeedsMachineAuth") return "warn";
    if (m?.state === "Starting") return "accent";
    if (m?.running) {
      if (m.error) return "warn";
      if (connected() || meshTunnel() || m.accessReady) return "success";
      return "warn";
    }
    return "muted";
  };

  const iconColor = () => {
    const t = tone();
    if (t === "success") return colors().success;
    if (t === "warn") return colors().warn;
    if (t === "danger") return colors().danger;
    if (t === "accent") return colors().accent;
    return colors().muted;
  };

  const badge = () => {
    const m = mesh();
    if (awaitingMeshPermission()) return tr("待授权", "Permission");
    if (m?.error && !m?.running) return tr("异常", "Error");
    if (m?.state === "NeedsLogin" || !!m?.authURL) return tr("去登录", "Sign in");
    if (m?.state === "NeedsMachineAuth") return tr("待审批", "Approval");
    if (m?.state === "Starting") return tr("连接中", "Connecting");
    if (m?.running) {
      if (connected() || meshTunnel() || m.accessReady) return tr("已连接", "Connected");
      return tr("待路由", "Pending");
    }
    return tr("未连接", "Disconnected");
  };

  const subtitle = () => {
    const m = mesh();
    if (awaitingMeshPermission()) {
      return tr("等待授予组网 VPN 权限以启用设备互通", "Waiting for mesh VPN permission to route device traffic");
    }
    if (m?.error && !m?.running) return m.error;
    if (m?.state === "NeedsLogin" || !!m?.authURL) {
      return tr("等待浏览器授权登录 Tailscale", "Waiting for Tailscale browser sign-in");
    }
    if (m?.state === "NeedsMachineAuth") {
      return tr("等待网络管理员审批此设备", "Waiting for tailnet administrator approval");
    }
    if (m?.state === "Starting") {
      return tr("正在连接 Tailscale 组网…", "Connecting to Tailscale mesh…");
    }
    if (m?.running) {
      const identity = m.selfIP || m.tailnet || "Tailscale";
      const peers = m.peerCount > 0 ? tr(`${m.peerCount} 台设备`, `${m.peerCount} peers`) : "";
      const routeText = m.error
        ? m.error
        : connected()
          ? tr("经当前 VPN 协同路由", "Routed with active VPN")
          : meshTunnel()
            ? tr("仅接管组网流量", "Mesh traffic only")
            : m.accessReady
              ? tr("组网路由已就绪", "Mesh routing ready")
              : tr("路由未就绪，点此查看", "Routing not ready, tap to view");
      return [identity, peers, routeText].filter(Boolean).join(" · ");
    }
    return tr("通过 Tailscale 互联自己的设备，独立于代理", "Connect your own devices via Tailscale, independent of proxy");
  };

  return <Group>
    <GroupRow
      testID="overview-mesh-card"
      icon="network"
      iconColor={iconColor()}
      title={tr("设备组网", "Mesh")}
      subtitle={subtitle()}
      value={badge()}
      valueTone={tone()}
      chevron
      onPress={props.onOpen}
    />
  </Group>;
}

export function MeshPage(props: { onBack: () => void }) {
  const snap = signal<MeshSnapshot | null>(null);
  const loaded = signal(false);
  const working = signal(false);
  const meshError = signal("");
  const tab = signal<MeshTab>("devices");
  const servicesDraft = signal("");
  const servicesDirty = signal(false);
  const search = signal("");
  const probePorts = signal<Record<string, string>>({});
  const probing = signal<Record<string, boolean>>({});
  const probeResults = signal<Record<string, MeshProbeResult>>({});
  const selectedDeviceId = signal("");
  const deviceSheetError = signal("");
  const portsSheetOpen = signal(false);
  const portsSheetError = signal("");
  const fallbackAuthNotice = signal("");

  let disposed = false;
  let requestSeq = 0;
  let userInitiatedConnect = false;
  let lastAutoOpenedAuthURL = "";

  cleanup(() => {
    disposed = true;
  });

  function applySnapshot(next: MeshSnapshot): boolean {
    snap.set(next);
    loaded.set(true);
    if (!servicesDirty()) servicesDraft.set(formatServices(next.status?.services));
    if (next.status?.running) {
      userInitiatedConnect = false;
      fallbackAuthNotice.set("");
    }
    const authURL = next.status?.authURL ?? "";
    if (
      androidHost() &&
      userInitiatedConnect &&
      !next.status?.running &&
      authURL &&
      authURL !== lastAutoOpenedAuthURL
    ) {
      lastAutoOpenedAuthURL = authURL;
      userInitiatedConnect = false;
      return true;
    }
    return false;
  }

  async function loadStatus() {
    const seq = ++requestSeq;
    const next = await call<MeshSnapshot>("mesh.status");
    if (disposed || seq !== requestSeq) return;
    if (applySnapshot(next)) void openLogin();
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
          const fast = !!snap()?.busy || st?.state === "Starting" || st?.state === "NeedsLogin" || awaitingMeshPermission();
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

  let lastTunnelKey = `${!!session()?.meshTunnel}|${!!session()?.connected}|${!!session()?.awaitingMeshPermission}`;
  effect(() => {
    const s = session();
    const key = `${!!s?.meshTunnel}|${!!s?.connected}|${!!s?.awaitingMeshPermission}`;
    if (key === lastTunnelKey) return;
    lastTunnelKey = key;
    if (loaded() && !working()) {
      void loadStatus().catch(() => {});
      void refreshStatus().catch(() => {});
    }
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
  const selectedDevice = () => {
    const id = selectedDeviceId();
    if (!id) return null;
    return devices().find(d => d.id === id) ?? null;
  };
  const selIsSelf = () => !!selectedDevice() && selectedDevice()!.id === status()?.self?.id;
  const selIP = () => deviceIP(selectedDevice() ?? undefined);
  const selBlocked = () => !!selectedDevice() && isBlocked(selectedDevice()!.id);
  const selPort = () => {
    const dev = selectedDevice();
    if (!dev) return "22";
    return probePorts()[dev.id] ?? defaultProbePort(dev);
  };
  const selProbe = () => {
    const dev = selectedDevice();
    return dev ? probeResults()[dev.id] : undefined;
  };
  const selProbeTone = (): "success" | "danger" | "info" => {
    const st = selProbe()?.state;
    if (st === "connected" || st === "verified") return "success";
    if (st === "failed") return "danger";
    return "info";
  };

  async function mutate(fn: () => Promise<void>, syncOverview = false, errorTarget?: (msg: string) => void) {
    if (working()) return;
    working.set(true);
    if (errorTarget) errorTarget(""); else meshError.set("");
    requestSeq++;
    try {
      await fn();
      if (syncOverview) void refreshStatus().catch(() => {});
    } catch (err) {
      if (!disposed) {
        haptic("reject");
        const text = message(err);
        if (errorTarget) errorTarget(text); else meshError.set(text);
      }
    } finally {
      if (!disposed) working.set(false);
    }
  }

  function runAction(action: "connect" | "disconnect" | "refresh" | "allow" | "block" | "shieldsUp" | "shieldsDown" | "clearHistory") {
    if (action === "disconnect") {
      userInitiatedConnect = false;
      lastAutoOpenedAuthURL = "";
      fallbackAuthNotice.set("");
    }
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.action", { action });
      if (!disposed && applySnapshot(next)) {
        await performOpenLogin();
      }
    }, action !== "clearHistory");
  }

  function beginConnect() {
    haptic("impact");
    userInitiatedConnect = true;
    lastAutoOpenedAuthURL = "";
    fallbackAuthNotice.set("");
    return runAction("connect");
  }

  async function performOpenLogin() {
    const res = await call<{ url?: string }>("mesh.action", { action: "login" });
    const url = res?.url || status()?.authURL || "";
    if (!url) throw new Error(tr("暂无可用的登录链接，请稍后重试。", "No sign-in URL is available yet. Try again shortly."));
    if (!/^https?:\/\//i.test(url)) throw new Error(tr("登录链接格式无效。", "Invalid sign-in URL."));
    if (androidHost()) {
      try {
        await androidCall("android.url.open", { url });
        if (!disposed) {
          fallbackAuthNotice.set("");
          showToast(tr("已打开浏览器授权页，完成登录后自动更新。", "Opened browser for sign-in. Status refreshes automatically."), 4000);
        }
      } catch {
        if (!disposed) {
          const msg = tr(`无法打开浏览器，请手动访问：${url}`, `Could not open browser. Visit: ${url}`);
          fallbackAuthNotice.set(msg);
          showToast(msg, 4000);
        }
      }
    } else {
      if (!disposed) {
        const msg = tr(`请在浏览器中打开登录链接：${url}`, `Open sign-in URL in browser: ${url}`);
        fallbackAuthNotice.set(msg);
        showToast(msg, 4000);
      }
    }
    await loadStatus().catch(() => {});
  }

  function openLogin() {
    return mutate(performOpenLogin);
  }

  function openPortsSheet() {
    portsSheetError.set("");
    if (!servicesDirty()) servicesDraft.set(formatServices(status()?.services));
    portsSheetOpen.set(true);
  }

  function openServices() {
    const raw = servicesDraft().trim();
    if (!raw) return;
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.services", { services: raw, enabled: true });
      if (!disposed) {
        servicesDirty.set(false);
        portsSheetOpen.set(false);
        portsSheetError.set("");
        applySnapshot(next);
        haptic("confirm");
        showToast(tr("已开放并保存授权端口。", "Authorized ports saved and opened."));
      }
    }, true, msg => portsSheetError.set(msg));
  }

  function toggleAutoConnect() {
    const nextEnabled = !snap()?.autoConnect;
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.autoConnect", { enabled: nextEnabled });
      if (!disposed) {
        applySnapshot(next);
        showToast(nextEnabled ? tr("已开启启动时自动连接", "Auto-connect enabled") : tr("已关闭启动时自动连接", "Auto-connect disabled"));
      }
    });
  }

  function toggleBlock(id: string, nextBlocked: boolean, fromSheet = false) {
    return mutate(async () => {
      const next = await call<MeshSnapshot>("mesh.block", { id, blocked: nextBlocked });
      if (!disposed) {
        applySnapshot(next);
        haptic("confirm");
        showToast(nextBlocked ? tr("已在本机封禁该设备。", "Device blocked on this device.") : tr("已解除对该设备的封禁。", "Device unblocked."));
      }
    }, false, fromSheet ? msg => deviceSheetError.set(msg) : undefined);
  }

  async function confirmToggleBlock(device: MeshDevice, nextBlocked: boolean) {
    if (nextBlocked) {
      const confirmed = await confirmDialog({
        title: tr("封禁此设备？", "Block this device?"),
        description: tr(
          `将仅在本机阻止「${device.name}」的组网通信，不会将其移出 Tailscale 网络。`,
          `Blocks mesh traffic with "${device.name}" on this device only. It remains in your tailnet.`,
        ),
        confirmLabel: tr("封禁设备", "Block device"),
        destructive: true,
      });
      if (!confirmed || disposed) return;
    }
    await toggleBlock(device.id, nextBlocked, true);
  }

  async function confirmClearHistory() {
    const confirmed = await confirmDialog({
      title: tr("清空本机访问记录？", "Clear local access history?"),
      description: tr("清空后无法恢复；已授权端口与封禁设备设置保留。", "Cleared history cannot be recovered. Authorized ports and blocked devices are retained."),
      confirmLabel: tr("清空记录", "Clear history"),
      destructive: true,
    });
    if (!confirmed || disposed) return;
    await runAction("clearHistory");
    if (!disposed) { haptic("confirm"); showToast(tr("访问记录已清空", "Access history cleared")); }
  }

  async function copyText(text: string, copiedMsg: string, fallbackMsg: string) {
    if (!text) return;
    if (androidHost()) {
      try {
        await androidCall("android.clipboard.write", { text });
        if (!disposed) { haptic("confirm"); showToast(copiedMsg, 3200); }
        return;
      } catch {
        // Fall through to showing text directly when clipboard bridge is unavailable.
      }
    }
    if (!disposed) showToast(fallbackMsg, 4000);
  }

  function copyIP(ip: string) {
    return copyText(ip, tr(`已复制组网 IP：${ip}`, `Copied mesh IP: ${ip}`), tr(`组网 IP：${ip}`, `Mesh IP: ${ip}`));
  }

  function copyAddress(ip: string, port: string) {
    const addr = `${ip}:${port}`;
    return copyText(addr, tr(`已复制地址：${addr}`, `Copied address: ${addr}`), tr(`连接地址：${addr}`, `Address: ${addr}`));
  }

  function copyAuthURL() {
    const url = status()?.authURL ?? "";
    return copyText(url, tr("已复制登录链接", "Copied sign-in URL"), tr(`登录链接：${url}`, `Sign-in URL: ${url}`));
  }

  async function probeDevice(device: MeshDevice) {
    const rawPort = (probePorts()[device.id] ?? defaultProbePort(device)).trim();
    const port = Number(rawPort);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      haptic("reject");
      deviceSheetError.set(tr("检测端口须为 1–65535 的整数。", "Probe port must be an integer between 1 and 65535."));
      return;
    }
    haptic("impact");
    deviceSheetError.set("");
    probing.set({ ...probing(), [device.id]: true });
    try {
      const result = await call<MeshProbeResult>("mesh.probe", { id: device.id, protocol: probeProtocol(port), port });
      if (!disposed) {
        probeResults.set({ ...probeResults(), [device.id]: result });
        haptic(result.state === "connected" || result.state === "verified" ? "confirm" : "reject");
      }
    } catch (err) {
      if (!disposed) {
        haptic("reject");
        deviceSheetError.set(message(err));
      }
    } finally {
      if (!disposed) {
        const next = { ...probing() };
        delete next[device.id];
        probing.set(next);
      }
    }
  }

  function openDeviceSheet(device: MeshDevice) {
    deviceSheetError.set("");
    selectedDeviceId.set(device.id);
  }

  function connectionStatusText(): string {
    const st = status();
    if (awaitingMeshPermission()) return tr("等待 VPN 权限", "Waiting for VPN permission");
    if (st?.running) return tr("已连接 Tailscale", "Connected to Tailscale");
    if (snap()?.busy || st?.state === "Starting") return tr("正在连接…", "Connecting…");
    if (st?.state === "NeedsLogin" || !!st?.authURL) return tr("等待浏览器授权登录", "Waiting for browser sign-in");
    if (st?.state === "NeedsMachineAuth") return tr("等待管理员审批", "Waiting for machine approval");
    return loaded() ? tr("尚未连接组网", "Mesh disconnected") : tr("正在读取状态…", "Loading status…");
  }

  function connectionStatusTone(): Tone {
    const st = status();
    if (awaitingMeshPermission()) return "warn";
    if (st?.running) return "success";
    if (snap()?.busy || st?.state === "Starting" || st?.state === "NeedsLogin" || !!st?.authURL) return "accent";
    if (st?.state === "NeedsMachineAuth") return "warn";
    return "muted";
  }

  function routeTag(): { label: string; tone: Tone } | null {
    if (!status()?.running) return null;
    if (awaitingMeshPermission()) return { label: tr("等待 VPN 权限", "VPN permission"), tone: "warn" };
    if (snap()?.accessReady) {
      if (connected()) return { label: tr("协同路由", "VPN + Mesh"), tone: "success" };
      if (meshTunnel()) return { label: tr("仅组网隧道", "Mesh tunnel"), tone: "success" };
      return { label: tr("路由就绪", "Route ready"), tone: "success" };
    }
    return { label: tr("路由未就绪", "Route pending"), tone: "warn" };
  }

  function routingSummary(): { text: string; badge: string; tone: Tone } {
    if (!status()?.running) {
      return {
        text: tr("连接组网后自动配置组网路由；切到直连或不开代理均不影响组网。", "Mesh routes activate once connected; works in Direct mode or without proxy."),
        badge: tr("未连接", "Off"),
        tone: "muted",
      };
    }
    if (awaitingMeshPermission()) {
      return {
        text: tr("系统正在请求 VPN 权限，用于仅转发组网设备虚拟 IP。", "Waiting for system VPN permission to route mesh virtual IPs only."),
        badge: tr("待授权", "Permission"),
        tone: "warn",
      };
    }
    if (snap()?.accessReady) {
      if (connected()) {
        return {
          text: tr("当前与代理 VPN 协同运行，组网设备优先走直连隧道。", "Running alongside proxy VPN; mesh devices route directly over tailnet."),
          badge: tr("协同路由", "VPN + Mesh"),
          tone: "success",
        };
      }
      if (meshTunnel()) {
        return {
          text: tr("独立组网隧道运行中，仅接管组网虚拟 IP，普通上网保持直连。", "Mesh-only tunnel active; only tailnet IPs are routed, regular traffic stays direct."),
          badge: tr("仅组网隧道", "Mesh tunnel"),
          tone: "success",
        };
      }
      return {
        text: tr("设备路由已就绪，可直接互通组网设备。", "Device routing is ready for mesh peers."),
        badge: tr("已就绪", "Ready"),
        tone: "success",
      };
    }
    return {
      text: androidHost()
        ? tr("组网已连但设备路由未就绪。可重新请求 VPN 权限，或检查应用范围设置。", "Mesh connected but routing is not ready. Retry VPN permission or check App scope.")
        : tr("组网已连；在连接页启动本地代理后可转发设备组网流量。", "Mesh connected. Start the local proxy from Connect to route mesh traffic."),
      badge: tr("未就绪", "Pending"),
      tone: "warn",
    };
  }

  function goBack() {
    void refreshStatus().catch(() => {});
    props.onBack();
  }

  const combinedError = () => meshError() || snap()?.error || snap()?.monitorError || "";
  const servicesText = () => formatServices(status()?.services);
  const selfIP = () => deviceIP(status()?.self);
  const selfIdentity = () => [selfIP(), status()?.tailnet].filter(Boolean).join(" · ");

  const tabItems = () => [
    {
      id: "devices",
      label: devices().length > 0 ? tr(`设备 (${devices().length})`, `Devices (${devices().length})`) : tr("设备", "Devices"),
      testID: "mesh-tab-devices",
    },
    {
      id: "sharing",
      label: allowed()
        ? tr("本机共享 · 开", "Sharing · On")
        : !status()?.shieldsKnown && !!status()?.running
          ? tr("本机共享 · 待确认", "Sharing · ?")
          : tr("本机共享", "Sharing"),
      testID: "mesh-tab-sharing",
    },
    {
      id: "settings",
      label: tr("设置", "Settings"),
      testID: "mesh-tab-settings",
    },
  ];

  function SetupGuide() {
    const step1Done = () => !!status()?.running;
    const step1Waiting = () => !!status()?.authURL || status()?.state === "NeedsLogin" || status()?.state === "NeedsMachineAuth" || !!snap()?.busy || status()?.state === "Starting";
    return <View style={{ paddingHorizontal: 16, paddingVertical: 12, gap: 12 }}>
      <Group>
        <GroupRow
          icon="network"
          iconColor={step1Done() ? colors().success : step1Waiting() ? colors().accent : colors().muted}
          title={tr("1 · 连接并授权 Tailscale 账号", "1 · Connect and sign in to Tailscale")}
          subtitle={
            status()?.state === "NeedsMachineAuth"
              ? tr("正在等待网络管理员审批此设备，批准后自动上线。", "Waiting for your tailnet administrator to approve this device.")
              : !!status()?.authURL || status()?.state === "NeedsLogin"
                ? tr("请在浏览器完成账号授权，返回应用后自动连接。", "Finish signing in via your browser. Status updates automatically.")
                : tr("点击上方「连接组网」，用官方账号加入私有设备网络。", "Tap Connect mesh above to sign in with your official account and join your private tailnet.")
          }
          value={step1Done() ? tr("已完成", "Done") : step1Waiting() ? tr("进行中", "Active") : undefined}
          valueTone={step1Done() ? "success" : "accent"}
        >
          <If when={!!status()?.authURL}>
            <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
              <Action testID="mesh-copy-auth-url" size="compact" label={tr("复制登录链接", "Copy sign-in URL")} disabled={working()} onPress={() => void copyAuthURL()} />
            </View>
          </If>
        </GroupRow>
        <GroupRow
          icon="route"
          iconColor={snap()?.accessReady ? colors().success : awaitingMeshPermission() ? colors().warn : colors().muted}
          title={tr("2 · 自动启用设备组网路由", "2 · Automatic device mesh routing")}
          subtitle={
            androidHost()
              ? tr("未开代理时自动拉起仅组网隧道，仅转发组网虚拟 IP；首次使用可能请求系统 VPN 权限。", "Starts a mesh-only tunnel automatically when proxy is off; first use may request VPN permission.")
              : tr("切到直连或不开代理均不影响组网；启动本地代理即可转发设备组网流量。", "Works in Direct mode or without a proxy profile once local routing runs.")
          }
        />
        <GroupRow
          icon="shield"
          title={tr("3 · 默认保护本机入站访问", "3 · Inbound access closed by default")}
          subtitle={tr("首次连接自动关闭入站端口，仍可主动访问其他设备；需要共享本机服务时前往「本机共享」授权端口。", "Inbound ports stay closed on first connect while outbound access works. Authorize explicit ports in Sharing when needed.")}
        />
      </Group>
    </View>;
  }

  return <View style={{ flex: 1, minHeight: 0, backgroundColor: colors().bg }}>
    <SubpageHeader
      title={tr("组网", "Mesh")}
      onBack={goBack}
      backTestID="mesh-back"
      action={() => <IconButton testID="mesh-refresh" icon="refresh" label={tr("刷新", "Refresh")} disabled={working()} onPress={() => { haptic("impact"); void runAction("refresh"); }} />}
    />

    <View style={{ paddingHorizontal: 16, paddingTop: 12, paddingBottom: 10, gap: 10 }}>
      <Panel padding={14} gap={10}>
        <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
          <StatusPill testID="mesh-status-pill" label={connectionStatusText()} tone={connectionStatusTone()} />
          <View style={{ flexDirection: "row", alignItems: "center", gap: 6, flexShrink: 0 }}>
            <If when={!status()?.running && !snap()?.busy && !status()?.authURL}>
              <Action testID="mesh-connect" primary size="compact" label={tr("连接组网", "Connect mesh")} disabled={working()} onPress={() => void beginConnect()} />
            </If>
            <If when={!!status()?.authURL}>
              <Action testID="mesh-login" primary={!status()?.running} size="compact" label={tr("浏览器登录", "Sign in via browser")} disabled={working()} onPress={() => void openLogin()} />
            </If>
            <If when={!!status()?.running || !!snap()?.busy || !!status()?.authURL}>
              <Action testID="mesh-disconnect" size="compact" label={status()?.running ? tr("断开", "Disconnect") : tr("取消", "Cancel")} disabled={working()} onPress={() => { haptic("impact"); void runAction("disconnect"); }} />
            </If>
          </View>
        </View>

        <If when={!!status()?.self?.name || !!selfIdentity()}>
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <Button
              testID="mesh-copy-self"
              disabled={!selfIP()}
              onPress={() => void copyIP(selfIP())}
              style={{ flex: 1, minWidth: 0, minHeight: 36, justifyContent: "center", borderRadius: 8 }}
              hitSlop={{ top: 4, bottom: 4, left: 4, right: 4 }}
              hoverStyle={{ backgroundColor: colors().hover }}
              activeStyle={{ opacity: 0.75 }}>
              <View style={{ gap: 2 }}>
                <If when={!!status()?.self?.name}>
                  <Text style={{ fontSize: 15, fontWeight: "600", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis" }}>{status()!.self.name}</Text>
                </If>
                <If when={!!selfIdentity()}>
                  <Text style={{ fontSize: 12, color: colors().muted, fontVariant: "tabular-nums", numberOfLines: 1, textOverflow: "ellipsis" }}>{selfIdentity()}</Text>
                </If>
              </View>
            </Button>
            <If when={!!routeTag()}>
              <Tag text={routeTag()!.label} tone={routeTag()!.tone} />
            </If>
          </View>
        </If>
      </Panel>

      <If when={!!combinedError()}>
        <Notice
          testID="mesh-error"
          tone="danger"
          text={combinedError()}
          action={meshError() ? () => <Action quiet size="compact" label={tr("关闭", "Dismiss")} disabled={false} onPress={() => meshError.set("")} /> : undefined}
        />
      </If>

      <If when={!combinedError() && !!fallbackAuthNotice()}>
        <Notice
          testID="mesh-auth-fallback"
          tone="info"
          text={fallbackAuthNotice()}
          action={() => <Action quiet size="compact" label={tr("关闭", "Dismiss")} disabled={false} onPress={() => fallbackAuthNotice.set("")} />}
        />
      </If>

      <If when={!combinedError() && awaitingMeshPermission()}>
        <Notice
          testID="mesh-permission-notice"
          tone="warn"
          text={tr("系统正在请求 VPN 权限，用于仅转发组网设备 IP（不接管普通上网）。", "Waiting for system VPN permission to route mesh IPs only (proxy stays off).")}
        />
      </If>

      <If when={!combinedError() && !awaitingMeshPermission() && !!status()?.running && !snap()?.accessReady}>
        <Notice
          testID="mesh-route-notice"
          tone={androidHost() ? "warn" : "info"}
          text={androidHost()
            ? tr("组网已连，但设备路由尚未就绪。可重新请求 VPN 权限，或检查应用范围设置。", "Mesh connected, but device routing is not ready. Retry VPN permission or check App scope.")
            : tr("组网已连；在连接页启动本地代理后可转发设备组网流量。", "Mesh connected. Start the local proxy from Connect to route mesh traffic.")}
          action={androidHost() ? () => <Action testID="mesh-retry-route" size="compact" label={tr("重试路由", "Retry route")} disabled={working()} onPress={() => { haptic("impact"); void runAction("refresh"); }} /> : undefined}
        />
      </If>

      <Segmented items={tabItems()} value={tab()} onChange={id => tab.set(id as MeshTab)} />
    </View>

    <If when={tab() === "devices"}>
      <View style={{ flex: 1, minHeight: 0 }}>
        <If when={devices().length > 0}>
          <View style={{ paddingHorizontal: 16, paddingBottom: 10 }}>
            <SearchField
              testID="mesh-device-search"
              value={search()}
              onChange={v => search.set(v)}
              placeholder={tr("搜索设备、账号、系统或 IP", "Search devices, account, OS, or IP")}
            />
          </View>
        </If>
        <List
          testID="mesh-device-list"
          each={filtered()}
          keyExtractor={d => d.id}
          refreshing={working()}
          onRefresh={() => { haptic("impact"); void runAction("refresh"); }}
          style={{ flex: 1 }}
          empty={
            !status()?.running && !devices().length
              ? <SetupGuide />
              : !devices().length
                ? <Empty
                    icon="network"
                    title={tr("暂无可显示的设备", "No devices to display")}
                    description={tr("同一网络中有权限查看的设备会显示在这里。", "Authorized devices in your tailnet appear here.")}
                  />
                : <Empty
                    icon="search"
                    title={tr("没有匹配的设备", "No matching devices")}
                    description={tr("换个关键词，或清除搜索条件。", "Try another keyword or reset the filter.")}
                    action={() => <If when={!!search().trim()}><Action label={tr("重置筛选", "Reset filter")} disabled={false} onPress={() => search.set("")} /></If>}
                  />
          }>
          {d => {
            const isSelf = () => d().id === status()?.self?.id;
            const ip = () => deviceIP(d());
            const blockedDevice = () => isBlocked(d().id);
            const meta = () => [ip() || "—", d().os, formatPathShort(d().path)].filter(Boolean).join(" · ");
            return <View style={{ paddingHorizontal: 16, paddingBottom: 8 }}>
              <View style={{
                flexDirection: "row", alignItems: "center", minHeight: 64,
                borderRadius: 12, borderWidth: 1, borderColor: colors().border,
                backgroundColor: colors().surface, overflow: "hidden",
              }}>
                <Button
                  testID={`mesh-device-${d().id}`}
                  onPress={() => openDeviceSheet(d())}
                  style={{ flex: 1, minHeight: 64 }}
                  hoverStyle={{ backgroundColor: colors().hover }}
                  activeStyle={{ opacity: 0.82 }}
                  focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}>
                  <View android={{ ripple: { color: tint(colors().accent, "1a") } }} style={{ flex: 1, minHeight: 64, flexDirection: "row", alignItems: "center", gap: 12, paddingLeft: 14, paddingRight: 6, paddingVertical: 10 }}>
                    <StatusDot tone={blockedDevice() ? "danger" : d().online ? "success" : "muted"} size={8} />
                    <View style={{ flex: 1, minWidth: 0, gap: 3 }}>
                      <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
                        <Text style={{ fontSize: 14, fontWeight: "600", color: colors().text, numberOfLines: 1, textOverflow: "ellipsis", flexShrink: 1 }}>{d().name}</Text>
                        <If when={isSelf()}><Tag text={tr("本机", "This device")} tone="accent" /></If>
                        <If when={blockedDevice()}><Tag text={tr("已封禁", "Blocked")} tone="danger" /></If>
                      </View>
                      <Text style={{ fontSize: 12, color: colors().muted, fontVariant: "tabular-nums", numberOfLines: 1, textOverflow: "ellipsis" }}>{meta()}</Text>
                    </View>
                  </View>
                </Button>
                <View style={{ flexDirection: "row", alignItems: "center", paddingRight: 8, gap: 2, flexShrink: 0 }}>
                  <Action testID={`mesh-copy-${d().id}`} quiet size="compact" label={tr("复制 IP", "Copy IP")} disabled={!ip()} onPress={() => void copyIP(ip())} />
                  <Icon name="chevronRight" size={16} color={colors().faint} />
                </View>
              </View>
            </View>;
          }}
        </List>
      </View>
    </If>

    <If when={tab() === "sharing"}>
      <Scroll testID="mesh-sharing-scroll" style={{ flex: 1 }} contentStyle={{ paddingHorizontal: 16, paddingBottom: 24, gap: 14 }} keyboardShouldPersistTaps="handled">
        <SectionLabel title={tr("本机入站访问", "Local inbound access")} />
        <If when={!status()?.shieldsKnown}>
          <Notice
            tone="warn"
            text={tr("入站防护状态待确认，请刷新或强制关闭入站访问。", "Inbound protection state is unknown. Refresh or force-close inbound access.")}
          />
          <Group>
            <GroupRow
              icon="shield"
              iconColor={colors().warn}
              title={tr("入站状态待确认", "Inbound state unknown")}
              subtitle={servicesText() || tr("尚未授权端口", "No authorized ports")}
            >
              <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
                <Action testID="mesh-services-edit" label={tr("授权端口", "Authorize ports")} disabled={busy()} onPress={openPortsSheet} />
                <Action testID="mesh-services-block" danger label={tr("关闭入站访问", "Close inbound access")} disabled={busy()} onPress={() => void runAction("block")} />
              </View>
            </GroupRow>
          </Group>
        </If>
        <If when={!!status()?.shieldsKnown}>
          <Group>
            <GroupRow
              icon="shield"
              iconColor={allowed() ? colors().success : colors().muted}
              title={tr("允许组网设备访问本机", "Allow inbound mesh access")}
              subtitle={allowed()
                ? (servicesText() ? tr(`已开放 · ${servicesText()}`, `Open · ${servicesText()}`) : tr("已开放", "Open"))
                : tr("入站访问已关闭；仍可主动访问其他设备", "Inbound access closed; outbound access remains available")}
              trailing={() => <ToggleSwitch
                testID="mesh-inbound-switch"
                checked={allowed()}
                disabled={busy()}
                label={tr("允许组网设备访问本机", "Allow inbound mesh access")}
                onChange={next => { if (next) openPortsSheet(); else void runAction("block"); }}
              />}
            />
            <GroupRow
              testID="mesh-services-edit"
              icon="server"
              title={tr("授权端口", "Authorized ports")}
              subtitle={servicesText() || tr("尚未配置授权端口", "No authorized ports configured")}
              value={allowed() ? tr("编辑", "Edit") : tr("配置", "Configure")}
              valueTone="accent"
              chevron
              disabled={busy()}
              onPress={openPortsSheet}
            />
          </Group>
        </If>
        <Label text={tr("授权只放行指定端口的组网流量，不会自动启动对应服务；本地代理端口不可开放。", "Authorization permits mesh traffic on explicit ports only and does not start services. Local proxy ports cannot be opened.")} muted size={11.5} />

        <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingTop: 4 }}>
          <SectionLabel title={tr("访问记录", "Access history")} trailing={(snap()?.visits ?? []).length ? String((snap()?.visits ?? []).length) : undefined} />
          <If when={(snap()?.visits ?? []).length > 0}>
            <Action testID="mesh-clear-history" quiet danger size="compact" label={tr("清空记录", "Clear history")} disabled={busy()} onPress={() => void confirmClearHistory()} />
          </If>
        </View>
        <If when={(snap()?.visits ?? []).length > 0}>
          <Group>
            <Each of={snap()?.visits ?? []} key={v => `${v.key}-${v.firstSeen}`}>
              {v => {
                const peer = () => devices().find(d => d.id === v().deviceId);
                return <GroupRow
                  title={v().device || v().remote || tr("未知设备", "Unknown device")}
                  subtitle={[v().owner || v().login, v().protocol, `${v().remote} → ${v().local}`].filter(Boolean).join(" · ")}
                  value={v().active ? tr("活跃", "Active") : formatTime(v().lastSeen)}
                  valueTone={v().active ? "success" : "muted"}
                  chevron={!!peer()}
                  onPress={peer() ? () => openDeviceSheet(peer()!) : undefined}
                />;
              }}
            </Each>
          </Group>
        </If>
        <If when={(snap()?.visits ?? []).length === 0}>
          <Panel>
            <Empty
              icon="activity"
              title={tr("暂无访问记录", "No access history")}
              description={tr("记录显示到本机的组网连接，不代表远程登录成功。", "Records show inbound mesh connections, not successful remote login.")}
            />
          </Panel>
        </If>
      </Scroll>
    </If>

    <If when={tab() === "settings"}>
      <Scroll testID="mesh-settings-scroll" style={{ flex: 1 }} contentStyle={{ paddingHorizontal: 16, paddingBottom: 24, gap: 14 }}>
        <SectionLabel title={tr("连接行为", "Connection behavior")} />
        <Group>
          <GroupRow
            icon="power"
            title={tr("启动时自动连接组网", "Connect mesh on startup")}
            subtitle={tr("使用已保存授权在启动时重连，不自动开放入站端口", "Reconnect on startup using saved credentials; inbound ports remain closed")}
            trailing={() => <ToggleSwitch
              testID="mesh-autoconnect"
              checked={!!snap()?.autoConnect}
              disabled={busy()}
              label={tr("启动时自动连接组网", "Connect mesh on startup")}
              onChange={() => void toggleAutoConnect()}
            />}
          />
          <GroupRow
            icon="route"
            title={tr("组网路由模式", "Mesh routing")}
            subtitle={routingSummary().text}
            value={routingSummary().badge}
            valueTone={routingSummary().tone}
          />
        </Group>

        <SectionLabel title={tr("已封禁设备", "Blocked devices")} trailing={String(blocked().length)} />
        <If when={blocked().length === 0}>
          <Group>
            <GroupRow
              icon="shield"
              title={tr("暂无封禁设备", "No blocked devices")}
              subtitle={tr("封禁仅在本机生效，不会把设备移出官方网络。", "Blocks apply on this device only; devices remain in your tailnet.")}
            />
          </Group>
        </If>
        <If when={blocked().length > 0}>
          <Group>
            <Each of={blocked()} key={id => id}>
              {id => {
                const match = () => devices().find(d => d.id === id());
                return <GroupRow
                  title={match()?.name ?? id()}
                  subtitle={match() ? (deviceIP(match()) || match()!.os || id()) : tr("当前不在设备列表中", "Not in current device list")}
                  trailing={() => <Action
                    testID={`mesh-unblock-${id()}`}
                    size="compact"
                    label={tr("解除封禁", "Unblock")}
                    disabled={busy()}
                    onPress={() => void toggleBlock(id(), false)}
                  />}
                />;
              }}
            </Each>
          </Group>
        </If>

        <If when={(status()?.health ?? []).length > 0}>
          <SectionLabel title={tr("运行诊断", "Health notices")} />
          <Notice tone="warn" text={(status()?.health ?? []).join("\n")} />
        </If>
      </Scroll>
    </If>

    <BottomSheet
      testID="mesh-device-sheet"
      closeTestID="mesh-device-sheet-close"
      open={!!selectedDevice()}
      onClose={() => { selectedDeviceId.set(""); deviceSheetError.set(""); }}
      scrollable
      title={selectedDevice()?.name ?? tr("设备详情", "Device details")}
      subtitle={[selectedDevice()?.owner || selectedDevice()?.login, deviceIP(selectedDevice() ?? undefined)].filter(Boolean).join(" · ")}
      error={deviceSheetError()}>
      <If when={!!selectedDevice()}>
        <View style={{ gap: 14 }}>
          <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 8 }}>
            <View style={{ flexDirection: "row", alignItems: "center", gap: 6 }}>
              <StatusPill
                label={selBlocked() ? tr("已封禁", "Blocked") : selectedDevice()?.online ? tr("在线", "Online") : tr("离线", "Offline")}
                tone={selBlocked() ? "danger" : selectedDevice()?.online ? "success" : "muted"}
              />
              <If when={selIsSelf()}><Tag text={tr("本机", "This device")} tone="accent" /></If>
            </View>
            <Action testID={`mesh-sheet-copy-${selectedDevice()?.id ?? ""}`} size="compact" label={tr("复制 IP", "Copy IP")} disabled={!selIP()} onPress={() => void copyIP(selIP())} />
          </View>

          <If when={!selIsSelf()}>
            <View style={{ gap: 10 }}>
              <SectionLabel title={tr("端口连通性检测", "Port reachability check")} />
                <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
                  <Each of={PROBE_PRESETS} key={p => p.port}>
                    {p => {
                      const active = () => selPort().trim() === p().port;
                      return <Button
                        disabled={!!probing()[selectedDevice()?.id ?? ""]}
                        hitSlop={{ top: 4, bottom: 4, left: 4, right: 4 }}
                        onPress={() => {
                          const dev = selectedDevice();
                          if (!dev) return;
                          deviceSheetError.set("");
                          probePorts.set({ ...probePorts(), [dev.id]: p().port });
                        }}
                        style={{
                          minHeight: 36, paddingHorizontal: 12, paddingVertical: 6,
                          borderRadius: RADIUS.control, alignItems: "center", justifyContent: "center",
                          backgroundColor: active() ? tint(colors().accent, "18") : colors().bg,
                          borderWidth: 1, borderColor: active() ? colors().accent : colors().border,
                        }}
                        hoverStyle={{ backgroundColor: colors().hover }}
                        activeStyle={{ opacity: 0.8 }}
                        focusStyle={{ outlineWidth: 2, outlineColor: colors().accent }}
                        disabledStyle={{ opacity: 0.4 }}>
                        <Text style={{ fontSize: 12.5, fontWeight: active() ? "600" : "500", color: active() ? colors().accent : colors().text, fontVariant: "tabular-nums" }}>{p().label}</Text>
                      </Button>;
                    }}
                  </Each>
                </View>
              <View style={{ gap: 6 }}>
                <Label text={tr("目标端口", "Target port")} bold size={12} muted />
                <Input
                  testID={`mesh-probe-port-${selectedDevice()?.id ?? ""}`}
                  value={selPort()}
                  onChange={({ value }) => {
                    const dev = selectedDevice();
                    if (dev) probePorts.set({ ...probePorts(), [dev.id]: value });
                  }}
                  placeholder="22"
                  keyboardType="number-pad"
                  autoCapitalize="none"
                  autoCorrect={false}
                  autoComplete="off"
                  accessibilityLabel={tr("检测端口", "Probe port")}
                  style={{
                    minHeight: 44, paddingHorizontal: 12, paddingVertical: 10,
                    borderWidth: 1, borderColor: colors().border, borderRadius: RADIUS.control,
                    color: colors().text, fontSize: 14, fontVariant: "tabular-nums",
                    backgroundColor: colors().bg, placeholderColor: colors().faint,
                  }}
                  focusStyle={{ borderColor: colors().accent }}
                />
              </View>
              <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
                <Action
                  testID={`mesh-probe-${selectedDevice()?.id ?? ""}`}
                  primary
                  label={probing()[selectedDevice()?.id ?? ""] ? tr("检测中…", "Probing…") : tr("检测连通性", "Check port")}
                  disabled={!!probing()[selectedDevice()?.id ?? ""] || !status()?.running || !selectedDevice()?.online || selBlocked()}
                  onPress={() => { const dev = selectedDevice(); if (dev) void probeDevice(dev); }}
                />
                <Action
                  testID={`mesh-copy-addr-${selectedDevice()?.id ?? ""}`}
                  label={tr("复制 IP:端口", "Copy IP:port")}
                  disabled={!selIP() || !selPort().trim()}
                  onPress={() => void copyAddress(selIP(), selPort().trim())}
                />
              </View>
              <If when={!!selProbe()}>
                <Notice
                  testID={`mesh-probe-result-${selectedDevice()?.id ?? ""}`}
                  tone={selProbeTone()}
                  text={`${selProbe()?.port ? `${selProbe()?.port}: ` : ""}${selProbe()?.message || selProbe()?.state || ""}`}
                />
              </If>
            </View>
          </If>

          <View style={{ gap: 8 }}>
            <SectionLabel title={tr("设备信息", "Device details")} />
            <Group>
              <GroupRow
                title={tr("虚拟 IP", "Mesh IP")}
                subtitle={(selectedDevice()?.ips ?? []).join("\n") || "—"}
                value={selIP() ? tr("复制", "Copy") : undefined}
                valueTone="accent"
                onPress={selIP() ? () => void copyIP(selIP()) : undefined}
              />
              <GroupRow title="DNS" subtitle={selectedDevice()?.dnsName || "—"} />
              <GroupRow title={tr("账号", "Account")} subtitle={selectedDevice()?.owner || selectedDevice()?.login || "—"} />
              <GroupRow title={tr("系统", "OS")} subtitle={selectedDevice()?.os || "—"} />
              <GroupRow title={tr("连接路径", "Connection path")} subtitle={formatPathShort(selectedDevice()?.path) || "—"} />
              <GroupRow title={tr("最近在线", "Last seen")} subtitle={formatTime(selectedDevice()?.lastSeen)} />
              <If when={(selectedDevice()?.tags ?? []).length > 0}>
                <GroupRow title={tr("标签", "Tags")} subtitle={(selectedDevice()?.tags ?? []).join(", ")} />
              </If>
            </Group>
          </View>

          <If when={!selIsSelf()}>
            <Action
              testID={`mesh-block-${selectedDevice()?.id ?? ""}`}
              full
              danger={!selBlocked()}
              label={selBlocked() ? tr("解除封禁", "Unblock device") : tr("封禁此设备", "Block device")}
              disabled={busy()}
              onPress={() => { const dev = selectedDevice(); if (dev) void confirmToggleBlock(dev, !selBlocked()); }}
            />
          </If>
        </View>
      </If>
    </BottomSheet>

    <BottomSheet
      testID="mesh-ports-sheet"
      closeTestID="mesh-ports-sheet-close"
      open={portsSheetOpen()}
      onClose={() => { portsSheetOpen.set(false); portsSheetError.set(""); }}
      scrollable
      title={tr("授权本机服务端口", "Authorize local service ports")}
      subtitle={tr("仅放行指定端口的组网流量，不会自动启动服务。", "Permits mesh traffic on explicit ports only; does not start services.")}
      error={portsSheetError()}
      footer={() => <View style={{ flexDirection: "row", gap: 8, justifyContent: "flex-end", flexWrap: "wrap" }}>
        <Action label={tr("取消", "Cancel")} disabled={busy()} onPress={() => { portsSheetOpen.set(false); portsSheetError.set(""); }} />
        <Action
          testID="mesh-services-open"
          primary
          label={tr("确认开放并保存", "Save & allow access")}
          disabled={busy() || !servicesDraft().trim()}
          onPress={() => void openServices()}
        />
      </View>}>
      <View style={{ gap: 12 }}>
        <View style={{ gap: 6 }}>
          <Label text={tr("常用端口预设", "Quick port presets")} bold size={12} muted />
          <View style={{ flexDirection: "row", gap: 8, flexWrap: "wrap" }}>
            <Each of={INBOUND_PRESETS} key={p => p}>
              {p => <Action
                size="compact"
                label={p()}
                disabled={busy()}
                onPress={() => {
                  servicesDirty.set(true);
                  servicesDraft.set(appendServicePreset(servicesDraft(), p()));
                }}
              />}
            </Each>
          </View>
        </View>
        <Field
          label={tr("授权端口", "Authorized ports")}
          testID="mesh-services-input"
          value={servicesDraft()}
          onChange={value => { servicesDirty.set(true); servicesDraft.set(value); }}
          placeholder="8080/tcp, 5555/tcp"
          helper={tr("多个端口用逗号分隔（支持 tcp / udp）；本地代理保留端口不可开放。", "Separate ports with commas (tcp / udp). Reserved local proxy ports cannot be opened.")}
          disabled={busy()}
        />
      </View>
    </BottomSheet>
  </View>;
}
