import type { Group, Profile, Status, Transport } from "./client";

// Explicitly synthetic. Only Preview.tsx and UI tests import this module.
export function fixtureTransport(empty = false): Transport {
  const profiles: Profile[] = empty ? [] : [{ id: "fixture-profile", name: "日常使用 / Everyday", kind: "local", source: "本地配置", updated: "2026-11-07T00:00:00Z", active: true }];
  const status: Status = { apiVersion: 1, running: !empty, activeProfile: empty ? "" : "fixture-profile", mixedPort: 17990, mode: "rule", systemProxy: false, tun: false, vpnSupported: false, capabilities: { systemProxy: false, tun: false }, traffic: { up: 0, down: 0, upTotal: 0, downTotal: 0, connections: 0, memory: 0, at: 0 } };
  const groups: Group[] = [{ name: "代理选择", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }, { name: "长节点名称用于检查换行 / A long example proxy name for wrapping", type: "Shadowsocks" }] }];
  groups.push({ name: "GLOBAL", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }] });
  const dispatch: Transport = async (method, params) => {
    switch (method) {
      case "engine.status": return status;
      case "profiles.list": return { active: status.activeProfile, profiles };
      case "proxies.groups": return { groups: status.activeProfile ? groups.map(g => ({ ...g, members: g.members.map(m => status.running ? m : { name: m.name, type: m.type }) })) : [], preview: !status.running };
      case "profiles.import": {
        const first = profiles.length === 0;
        const profile: Profile = { id: first ? "imported" : `imported-${profiles.length + 1}`, name: String(params.name), kind: params.url ? "url" : "local", source: params.url ? "example.invalid" : "本地配置", updated: "2026-11-07T00:00:00Z", active: first };
        profiles.push(profile);
        if (first) status.activeProfile = profile.id;
        return profile;
      }
      case "profiles.activate": status.activeProfile = String(params.id); status.running = true; profiles.forEach((p) => { p.active = p.id === status.activeProfile; }); groups.forEach(g => g.members.forEach(m => { delete m.delay; })); return status;
      case "engine.start": status.running = true; status.reconnectRequired = false; profiles.forEach(p => p.reconnectRequired = false); return status;
      case "engine.stop": status.running = false; return status;
      case "engine.mode": status.mode = params.mode as Status["mode"]; return status;
      case "profiles.editor": { const p = profiles.find(p => p.id === params.id)!; return { id: p.id, name: p.name, kind: p.kind, url: p.kind === "url" ? "https://example.invalid/profile.yaml" : "" }; }
      case "profiles.update": { const p = profiles.find(p => p.id === params.id)!; if (params.name) p.name = String(params.name); return { profile: p, saved: true, applied: false, status }; }
      case "profiles.refresh": { const p = profiles.find(p => p.id === params.id)!; p.reconnectRequired = p.active && status.running; status.reconnectRequired = !!p.reconnectRequired; return { profile: p, saved: true, applied: false, reconnectRequired: p.reconnectRequired, status }; }
      case "profiles.delete": { const i = profiles.findIndex(p => p.id === params.id); profiles.splice(i, 1); if (status.activeProfile === params.id) { status.activeProfile = profiles[0]?.id ?? ""; profiles.forEach(p => p.active = p.id === status.activeProfile); } return { saved: true, applied: false, status }; }
      case "logs.list": return { logs: [{ at: "2026-11-08T00:00:00Z", level: "info", text: "Example diagnostic log — no live traffic." }] };
      case "proxies.select": groups.find(g => g.name === params.group)!.now = String(params.member); return { ok: true };
      case "proxies.delay": groups.forEach(g => g.members.forEach(m => { if (m.name === params.name) m.delay = 12; })); return { name: params.name, delay: 12 };
      default: throw new Error(`Unknown fixture method: ${method}`);
    }
  };
  // Match the real JSON bridge: responses are snapshots, not live references
  // into the fixture's mutable model (signals compare array identity).
  return async (method, params) => JSON.parse(JSON.stringify(await dispatch(method, params)));
}
