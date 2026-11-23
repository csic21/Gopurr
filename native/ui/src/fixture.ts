import type { Group, Profile, Status, Transport } from "./client";
import type { MeshDevice, MeshService, MeshSnapshot } from "./mobile/mesh";

function emptyMeshDevice(): MeshDevice {
  return { id: "", name: "", dnsName: "", os: "", owner: "", login: "", ips: [], tags: [], online: false, active: false, lastSeen: "", path: "" };
}

function parseFixtureServices(raw: string): MeshService[] {
  const out: MeshService[] = [];
  for (const part of raw.split(/[\s,，]+/).map(v => v.trim()).filter(Boolean)) {
    const pieces = part.toLowerCase().split("/");
    const port = Number(pieces[0]);
    const protocol = pieces[1] ?? "tcp";
    if (pieces.length > 2 || !Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error("请输入明确端口，例如 8080/tcp, 5555/tcp");
    }
    if (protocol !== "tcp" && protocol !== "udp") {
      throw new Error("协议只能是 tcp 或 udp");
    }
    if (!out.some(s => s.port === port && s.protocol === protocol)) out.push({ protocol, port });
  }
  return out;
}

// Explicitly synthetic. Only Preview.tsx and UI tests import this module.
export function fixtureTransport(empty = false): Transport {
  const profiles: Profile[] = empty ? [] : [{ id: "fixture-profile", name: "日常使用 / Everyday", kind: "local", source: "本地配置", updated: "2026-11-07T00:00:00Z", active: true }];
  const status: Status = { apiVersion: 1, running: !empty, activeProfile: empty ? "" : "fixture-profile", mixedPort: 17990, mode: "rule", systemProxy: false, tun: false, vpnSupported: false, capabilities: { systemProxy: false, tun: false }, traffic: { up: 0, down: 0, upTotal: 0, downTotal: 0, connections: 0, memory: 0, at: 0 } };
  const mesh: MeshSnapshot = {
    managed: false, started: false, busy: false, action: "", autoConnect: false, accessReady: false, error: "", monitorError: "", updated: "2026-11-07T08:35:00Z",
    status: { installed: true, official: true, running: false, shieldsKnown: true, shieldsUp: true, state: "Stopped", version: "1.0.0", authURL: "", tailnet: "", self: emptyMeshDevice(), peers: [], services: [], blocked: [], health: [] },
    visits: [],
  };
  const syncMeshSummary = () => {
    mesh.accessReady = status.running && mesh.status.running;
    if (!mesh.managed && !mesh.status.running && mesh.status.state !== "NeedsLogin" && mesh.status.state !== "Starting") {
      delete status.mesh;
      return;
    }
    status.mesh = {
      managed: mesh.managed,
      running: mesh.status.running,
      state: mesh.status.state,
      selfName: mesh.status.self.name || undefined,
      selfIP: mesh.status.self.ips[0] || undefined,
      tailnet: mesh.status.tailnet || undefined,
      peerCount: mesh.status.peers.length,
      shieldsUp: mesh.status.shieldsUp,
      authURL: mesh.status.authURL || undefined,
      accessReady: mesh.accessReady,
      error: mesh.error || undefined,
    };
  };
  const sampleSelf = (): MeshDevice => ({ id: "self", name: "Android Phone", dnsName: "android-phone.example.ts.net.", os: "android", owner: "user@example.com", login: "user@example.com", ips: ["100.64.0.2"], tags: [], online: true, active: true, lastSeen: "2026-11-07T08:35:00Z", path: "direct" });
  const samplePeers = (): MeshDevice[] => [
    { id: "peer-1", name: "MacBook Pro", dnsName: "macbook-pro.example.ts.net.", os: "macOS", owner: "user@example.com", login: "user@example.com", ips: ["100.64.0.1"], tags: [], online: true, active: true, lastSeen: "2026-11-07T08:35:00Z", path: "direct" },
    { id: "peer-2", name: "Home Server", dnsName: "home-server.example.ts.net.", os: "linux", owner: "ops@example.com", login: "ops@example.com", ips: ["100.64.0.3"], tags: ["tag:server"], online: false, active: false, lastSeen: "2026-11-07T07:00:00Z", path: "derp(sin)" },
  ];
  const groups: Group[] = [{ name: "代理选择", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }, { name: "长节点名称用于检查换行 / A long example proxy name for wrapping", type: "Shadowsocks" }] }];
  groups.push({ name: "GLOBAL", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }] });
  const dispatch: Transport = async (method, params) => {
    switch (method) {
      case "engine.status": syncMeshSummary(); return status;
      case "profiles.list": return { active: status.activeProfile, profiles };
      case "proxies.groups": return { groups: status.activeProfile ? groups.map(g => ({ ...g, members: g.members.map(m => status.running ? m : { name: m.name, type: m.type }) })) : [], preview: !status.running };
      case "profiles.import": {
        const first = profiles.length === 0;
        const profile: Profile = { id: first ? "imported" : `imported-${profiles.length + 1}`, name: String(params.name), kind: params.url ? "url" : "local", source: params.url ? "example.invalid" : "本地配置", updated: "2026-11-07T00:00:00Z", active: first };
        profiles.push(profile);
        if (first) status.activeProfile = profile.id;
        return profile;
      }
      case "profiles.activate": status.activeProfile = String(params.id); status.running = true; profiles.forEach((p) => { p.active = p.id === status.activeProfile; }); groups.forEach(g => g.members.forEach(m => { delete m.delay; })); syncMeshSummary(); return status;
      case "engine.start":
        if (!status.activeProfile && !mesh.managed && !mesh.status.running) throw new Error("请先导入并选择配置");
        status.running = true; status.reconnectRequired = false; profiles.forEach(p => p.reconnectRequired = false); syncMeshSummary(); return status;
      case "engine.stop": status.running = false; syncMeshSummary(); return status;
      case "engine.mode": status.mode = params.mode as Status["mode"]; syncMeshSummary(); return status;
      case "profiles.editor": { const p = profiles.find(p => p.id === params.id)!; return { id: p.id, name: p.name, kind: p.kind, url: p.kind === "url" ? "https://example.invalid/profile.yaml" : "" }; }
      case "profiles.update": { const p = profiles.find(p => p.id === params.id)!; if (params.name) p.name = String(params.name); return { profile: p, saved: true, applied: false, status }; }
      case "profiles.refresh": { const p = profiles.find(p => p.id === params.id)!; p.reconnectRequired = p.active && status.running; status.reconnectRequired = !!p.reconnectRequired; return { profile: p, saved: true, applied: false, reconnectRequired: p.reconnectRequired, status }; }
      case "profiles.delete": { const i = profiles.findIndex(p => p.id === params.id); profiles.splice(i, 1); if (status.activeProfile === params.id) { status.activeProfile = profiles[0]?.id ?? ""; profiles.forEach(p => p.active = p.id === status.activeProfile); } return { saved: true, applied: false, status }; }
      case "logs.list": return { logs: [{ at: "2026-11-08T00:00:00Z", level: "info", text: "Example diagnostic log — no live traffic." }] };
      case "proxies.select": groups.find(g => g.name === params.group)!.now = String(params.member); return { ok: true };
      case "proxies.delay": groups.forEach(g => g.members.forEach(m => { if (m.name === params.name) m.delay = 12; })); return { name: params.name, delay: 12 };
      case "mesh.status": syncMeshSummary(); return mesh;
      case "mesh.action": {
        const act = String(params.action ?? "");
        switch (act) {
          case "connect":
            mesh.managed = true; mesh.started = true; mesh.error = "";
            mesh.status.running = true; mesh.status.state = "Running"; mesh.status.authURL = ""; mesh.status.tailnet = "example.ts.net"; mesh.status.shieldsKnown = true;
            mesh.status.self = sampleSelf(); mesh.status.peers = samplePeers();
            break;
          case "disconnect":
            mesh.managed = true; mesh.started = true;
            mesh.status.running = false; mesh.status.state = "Stopped"; mesh.status.authURL = ""; mesh.status.tailnet = ""; mesh.status.shieldsUp = true;
            mesh.status.self = emptyMeshDevice(); mesh.status.peers = [];
            break;
          case "refresh":
            mesh.updated = new Date().toISOString();
            break;
          case "allow":
          case "shieldsDown":
            mesh.managed = true; mesh.status.shieldsKnown = true; mesh.status.shieldsUp = false;
            break;
          case "block":
          case "shieldsUp":
            mesh.managed = true; mesh.status.shieldsKnown = true; mesh.status.shieldsUp = true;
            break;
          case "clearHistory":
            mesh.visits = [];
            break;
          case "login":
            return { url: mesh.status.authURL || "https://login.tailscale.com/a/example" };
          default:
            throw new Error(`Unknown mesh action: ${act}`);
        }
        syncMeshSummary();
        return mesh;
      }
      case "mesh.services": {
        const parsed = parseFixtureServices(String(params.services ?? ""));
        mesh.managed = true;
        mesh.status.services = parsed;
        mesh.status.shieldsKnown = true;
        mesh.status.shieldsUp = !params.enabled;
        syncMeshSummary();
        return mesh;
      }
      case "mesh.block": {
        const id = String(params.id ?? "");
        const block = !!params.blocked;
        mesh.managed = true;
        mesh.status.blocked = block
          ? [...new Set([...mesh.status.blocked, id])]
          : mesh.status.blocked.filter(v => v !== id);
        syncMeshSummary();
        return mesh;
      }
      case "mesh.autoConnect":
        mesh.managed = true;
        mesh.autoConnect = !!params.enabled;
        syncMeshSummary();
        return mesh;
      case "mesh.probe": {
        const id = String(params.id ?? "");
        const port = Number(params.port ?? 0);
        const peer = mesh.status.peers.find(p => p.id === id);
        const checked = new Date().toISOString();
        if (!mesh.status.running || !peer) {
          return { deviceId: id, state: "unknown", message: "设备或组网连接已变化，请刷新后重试", port, checked };
        }
        if (!peer.online) {
          return { deviceId: id, state: "failed", message: "未连通，请检查对方的端口授权、服务和网络", port, checked };
        }
        return { deviceId: id, state: "connected", message: "TCP 端口已连接；尚未验证服务类型、授权或登录", port, checked };
      }
      default: throw new Error(`Unknown fixture method: ${method}`);
    }
  };
  // Match the real JSON bridge: responses are snapshots, not live references
  // into the fixture's mutable model (signals compare array identity).
  return async (method, params) => JSON.parse(JSON.stringify(await dispatch(method, params)));
}
