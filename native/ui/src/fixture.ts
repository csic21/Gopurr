import type {
  Group, ParsedProxyNode, ParseFailure, ParseResult, Profile,
  ProxyEditorGroup, ProxyEditorNode, ProxyEditorPayload, ProxyEdits, Status, Transport,
} from "./client";
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

function parseFixtureProxyUri(line: string): ParsedProxyNode | null {
  const trimmed = line.trim();
  const match = trimmed.match(/^(vless|vmess|trojan|ss|ssr|hysteria2|hy2|tuic|wireguard|wg|socks5|socks|http|https):\/\/(.+)$/i);
  if (!match) return null;
  const rawScheme = match[1].toLowerCase();
  const type = rawScheme === "hy2" ? "hysteria2" : rawScheme === "wg" ? "wireguard" : rawScheme === "socks" ? "socks5" : rawScheme === "https" ? "http" : rawScheme;
  const hashIdx = trimmed.indexOf("#");
  const fragment = hashIdx >= 0 ? decodeURIComponent(trimmed.slice(hashIdx + 1)) : "";
  const withoutHash = hashIdx >= 0 ? trimmed.slice(0, hashIdx) : trimmed;
  const afterScheme = withoutHash.slice(withoutHash.indexOf("://") + 3);
  const qIdx = afterScheme.indexOf("?");
  const authority = qIdx >= 0 ? afterScheme.slice(0, qIdx) : afterScheme;
  const atIdx = authority.lastIndexOf("@");
  const cred = atIdx >= 0 ? authority.slice(0, atIdx) : "";
  const hostPort = atIdx >= 0 ? authority.slice(atIdx + 1) : authority;
  const slashIdx = hostPort.indexOf("/");
  const cleanHostPort = slashIdx >= 0 ? hostPort.slice(0, slashIdx) : hostPort;
  const colonIdx = cleanHostPort.lastIndexOf(":");
  const server = colonIdx > 0 ? cleanHostPort.slice(0, colonIdx) : cleanHostPort;
  const port = colonIdx > 0 ? Number(cleanHostPort.slice(colonIdx + 1)) : (type === "http" ? 80 : 443);
  if (!server || !Number.isInteger(port) || port < 1 || port > 65535) return null;
  const options: Record<string, unknown> = { type };
  if (type === "vless" || type === "vmess" || type === "tuic") options.uuid = cred || "00000000-0000-0000-0000-000000000001";
  else if (cred) options.password = cred;
  if (type === "ss") options.cipher = "aes-128-gcm";
  return { name: fragment || `${type.toUpperCase()}-${server}`, type, server, port, options };
}

function parseFixtureInputs(text: string, mode = "auto"): ParseResult {
  const trimmed = text.trim();
  if (!trimmed) return { kind: "empty" };
  const lines = trimmed.split(/\r?\n/).map(l => l.trim()).filter(Boolean);
  if (lines.length === 1 && /^https?:\/\//i.test(lines[0])) {
    const hasUserinfo = /^https?:\/\/[^/\s@]+@[^/\s]+/i.test(lines[0]);
    if (mode === "nodes" || (mode === "auto" && hasUserinfo)) {
      const node = parseFixtureProxyUri(lines[0]);
      if (node) return { kind: "nodes", nodes: [node], ambiguousHttp: !hasUserinfo };
    }
    if (mode !== "nodes") {
      return { kind: "url", url: lines[0], ambiguousHttp: true };
    }
  }
  if (/^\s*(proxies|proxy-groups|rules)\s*:/m.test(trimmed)) {
    return { kind: "clash_yaml", yaml: trimmed };
  }
  const nodes: ParsedProxyNode[] = [];
  const failures: ParseFailure[] = [];
  for (let i = 0; i < lines.length; i++) {
    const parsed = parseFixtureProxyUri(lines[i]);
    if (parsed) nodes.push(parsed);
    else failures.push({ line: i + 1, raw: lines[i], reason: "不支持的链接格式 / Unsupported URI" });
  }
  if (nodes.length > 0) return { kind: "nodes", nodes, ...(failures.length ? { failures } : {}) };
  if (failures.length > 0) return { kind: "unknown", failures };
  return { kind: "unknown" };
}

interface FixtureProfileState {
  profile: Profile;
  url: string;
  baseNodes: ProxyEditorNode[];
  baseGroups: ProxyEditorGroup[];
  edits: ProxyEdits | null;
}

// Explicitly synthetic. Only Preview.tsx and UI tests import this module.
export function fixtureTransport(empty = false): Transport {
  const futureExpire = Math.floor(Date.now() / 1000) + 23 * 86400;
  const makeDefaultState = (p: Profile, url = ""): FixtureProfileState => ({
    profile: p,
    url,
    baseNodes: [
      {
        source: "示例节点 / Example proxy",
        name: "示例节点 / Example proxy",
        type: "ss",
        server: "hk1.example.invalid",
        port: 8388,
        options: { type: "ss", cipher: "aes-128-gcm", password: "demo-password" },
        edited: false,
        missing: false,
        base: { name: "示例节点 / Example proxy", type: "ss", server: "hk1.example.invalid", port: 8388, options: { type: "ss", cipher: "aes-128-gcm", password: "demo-password" } },
      },
      {
        source: "长节点名称用于检查换行 / A long example proxy name for wrapping",
        name: "长节点名称用于检查换行 / A long example proxy name for wrapping",
        type: "vless",
        server: "jp-tokyo.example.invalid",
        port: 443,
        options: { type: "vless", uuid: "11111111-2222-3333-4444-555555555555", tls: true },
        edited: false,
        missing: false,
        base: { name: "长节点名称用于检查换行 / A long example proxy name for wrapping", type: "vless", server: "jp-tokyo.example.invalid", port: 443, options: { type: "vless", uuid: "11111111-2222-3333-4444-555555555555", tls: true } },
      },
    ],
    baseGroups: [
      {
        source: "代理选择",
        name: "代理选择",
        type: "select",
        members: ["DIRECT", "示例节点 / Example proxy", "长节点名称用于检查换行 / A long example proxy name for wrapping"],
        inheritedMembers: ["DIRECT", "示例节点 / Example proxy", "长节点名称用于检查换行 / A long example proxy name for wrapping"],
        appendMembers: [],
        explicit: false,
        explicitMembers: false,
        edited: false,
        missing: false,
        deleted: false,
        use: [],
        includeAll: false,
        base: { name: "代理选择", type: "select", members: ["DIRECT", "示例节点 / Example proxy", "长节点名称用于检查换行 / A long example proxy name for wrapping"] },
      },
    ],
    edits: null,
  });

  const profileStates: FixtureProfileState[] = empty ? [] : [
    makeDefaultState(
      {
        id: "fixture-profile",
        name: "日常使用 / Everyday",
        kind: "url",
        source: "sub.example.invalid",
        updated: "2026-11-07T00:00:00Z",
        active: true,
        info: { upload: Math.round(14.2 * 1024 ** 3), download: Math.round(28.4 * 1024 ** 3), total: 200 * 1024 ** 3, expire: futureExpire },
        updateHours: 12,
        nodeCount: 2,
        localNodeCount: 0,
        editedNodeCount: 0,
        editedGroupCount: 0,
        extraRuleCount: 0,
        templateCount: 0,
        isAggregate: false,
        hasPath: false,
        isPureLocal: false,
        dependentAggregates: [],
      },
      "https://sub.example.invalid/profile.yaml",
    ),
  ];

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

  const groups: Group[] = [
    { name: "代理选择", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }, { name: "长节点名称用于检查换行 / A long example proxy name for wrapping", type: "Shadowsocks" }] },
    { name: "GLOBAL", type: "Selector", now: "DIRECT", hidden: false, members: [{ name: "DIRECT", type: "Direct" }, { name: "示例节点 / Example proxy", type: "Shadowsocks" }] },
  ];

  function buildEditorPayload(st: FixtureProfileState): ProxyEditorPayload {
    const edits = st.edits;
    const nodes: ProxyEditorNode[] = st.baseNodes.map(n => ({ ...n, options: { ...n.options } }));
    let localCount = 0;
    let editedNodeCount = 0;

    for (const ne of edits?.nodes ?? []) {
      if (!ne.source) {
        localCount++;
        nodes.push({
          source: "",
          localId: ne.localId,
          name: ne.name || "Local Node",
          type: String(ne.options?.type ?? "vless"),
          server: ne.server || "",
          port: ne.port || 443,
          options: { ...(ne.options ?? {}) },
          edited: true,
          missing: false,
          base: null,
        });
      } else if (ne.delete) {
        const idx = nodes.findIndex(n => n.source === ne.source);
        if (idx >= 0) nodes.splice(idx, 1);
      } else {
        const target = nodes.find(n => n.source === ne.source);
        if (target) {
          editedNodeCount++;
          target.edited = true;
          if (ne.name) target.name = ne.name;
          if (ne.server) target.server = ne.server;
          if (ne.port) target.port = ne.port;
          if (ne.options) {
            target.options = { ...target.options, ...ne.options };
            if (typeof ne.options.type === "string") target.type = ne.options.type;
          }
        }
      }
    }

    const editorGroups: ProxyEditorGroup[] = st.baseGroups.map(g => ({
      ...g,
      members: [...g.members],
      inheritedMembers: [...g.inheritedMembers],
      appendMembers: [...g.appendMembers],
      use: [...g.use],
    }));

    let editedGroupCount = 0;
    for (const ge of edits?.groups ?? []) {
      editedGroupCount++;
      if (!ge.source) {
        editorGroups.push({
          source: "",
          name: ge.name || "New Group",
          type: ge.type || "select",
          members: [...(ge.members ?? [])],
          inheritedMembers: [],
          appendMembers: [],
          explicit: true,
          explicitMembers: true,
          edited: true,
          missing: false,
          deleted: false,
          use: [],
          includeAll: false,
          base: null,
        });
      } else {
        const target = editorGroups.find(g => g.source === ge.source);
        if (target) {
          target.edited = true;
          if (ge.delete) { target.deleted = true; continue; }
          if (ge.name) target.name = ge.name;
          if (ge.type) target.type = ge.type;
          if (ge.members) {
            target.members = [...ge.members];
            target.explicit = true;
            target.explicitMembers = true;
          } else if (ge.appendMembers?.length) {
            target.appendMembers = [...ge.appendMembers];
            const combined = [...target.inheritedMembers];
            for (const m of ge.appendMembers) if (!combined.includes(m)) combined.push(m);
            target.members = combined;
          }
        }
      }
    }

    st.profile.nodeCount = nodes.length;
    st.profile.localNodeCount = st.profile.isPureLocal ? 0 : localCount;
    st.profile.editedNodeCount = st.profile.isPureLocal ? 0 : editedNodeCount;
    st.profile.editedGroupCount = st.profile.isPureLocal ? 0 : editedGroupCount;

    const availableNames = ["DIRECT", "REJECT", ...nodes.map(n => n.name), ...editorGroups.filter(g => !g.deleted).map(g => g.name)];
    return {
      id: st.profile.id,
      profileId: st.profile.id,
      profileName: st.profile.name,
      profileKind: st.profile.kind,
      isPureLocal: !!st.profile.isPureLocal,
      nodes,
      groups: editorGroups,
      edits: st.edits,
      options: availableNames,
      availableNames,
      providerNodes: [],
    };
  }

  function syncRuntimeGroupsFromActive() {
    const active = profileStates.find(s => s.profile.id === status.activeProfile);
    if (!active) return;
    const ed = buildEditorPayload(active);
    const prevNow = new Map(groups.map(g => [g.name, g.now]));
    const prevDelay = new Map<string, number>();
    for (const g of groups) {
      for (const m of g.members) if (m.delay !== undefined) prevDelay.set(m.name, m.delay);
    }
    const nodeType = new Map(ed.nodes.map(n => [n.name, n.type === "ss" ? "Shadowsocks" : n.type]));

    groups.length = 0;
    for (const g of ed.groups) {
      if (g.deleted) continue;
      const members = g.members.map(name => ({
        name,
        type: name === "DIRECT" ? "Direct" : name === "REJECT" ? "Reject" : (nodeType.get(name) ?? "Proxy"),
        ...(prevDelay.has(name) ? { delay: prevDelay.get(name) } : {}),
      }));
      const currentNow = prevNow.get(g.name);
      groups.push({
        name: g.name,
        type: g.type === "select" ? "Selector" : g.type === "url-test" ? "URLTest" : g.type === "fallback" ? "Fallback" : "LoadBalance",
        now: currentNow && members.some(m => m.name === currentNow) ? currentNow : (members[0]?.name ?? ""),
        hidden: false,
        members,
      });
    }
    const globalMembers = [
      { name: "DIRECT", type: "Direct" },
      ...ed.nodes.map(n => ({ name: n.name, type: nodeType.get(n.name) ?? n.type, ...(prevDelay.has(n.name) ? { delay: prevDelay.get(n.name) } : {}) })),
    ];
    groups.push({ name: "GLOBAL", type: "Selector", now: prevNow.get("GLOBAL") ?? "DIRECT", hidden: false, members: globalMembers });
  }

  const dispatch: Transport = async (method, params) => {
    switch (method) {
      case "engine.status": syncMeshSummary(); return status;
      case "profiles.list": return { active: status.activeProfile, profiles: profileStates.map(s => s.profile) };
      case "proxies.groups": return { groups: status.activeProfile ? groups.map(g => ({ ...g, members: g.members.map(m => status.running ? m : { name: m.name, type: m.type }) })) : [], preview: !status.running };
      case "proxies.parse": return parseFixtureInputs(String(params.text ?? ""), String(params.mode ?? "auto"));
      case "proxies.editor": {
        const id = String(params.id || status.activeProfile || "");
        const st = profileStates.find(s => s.profile.id === id);
        if (!st) throw new Error("请先导入并选择配置");
        return buildEditorPayload(st);
      }
      case "proxies.save": {
        const id = String(params.id || status.activeProfile || "");
        const st = profileStates.find(s => s.profile.id === id);
        if (!st) throw new Error("请先导入并选择配置");
        const nextEdits = (params.edits as ProxyEdits | null) ?? null;
        if (st.profile.isPureLocal) {
          st.edits = nextEdits;
          const applied = buildEditorPayload(st);
          st.baseNodes = applied.nodes.map(n => ({ ...n, source: n.name, localId: undefined, edited: false, base: { name: n.name, type: n.type, server: n.server, port: n.port, options: { ...n.options } } }));
          st.baseGroups = applied.groups.filter(g => !g.deleted).map(g => ({ ...g, source: g.name, edited: false, appendMembers: [], inheritedMembers: [...g.members], explicit: false, explicitMembers: false, base: { name: g.name, type: g.type, members: [...g.members] } }));
          st.edits = null;
        } else {
          st.edits = nextEdits;
          buildEditorPayload(st);
        }
        if (id === status.activeProfile) {
          syncRuntimeGroupsFromActive();
          if (status.running) {
            st.profile.reconnectRequired = true;
            status.reconnectRequired = true;
          }
        }
        syncMeshSummary();
        return { profile: st.profile, saved: true, applied: false, reconnectRequired: !!st.profile.reconnectRequired, status };
      }
      case "profiles.createLocal": {
        const first = profileStates.length === 0;
        const shouldActivate = first || (!!params.activate && !status.running && !status.tun);
        const inputNodes = (Array.isArray(params.nodes) ? params.nodes : []) as ParsedProxyNode[];
        const id = first ? "local-1" : `local-${profileStates.length + 1}`;
        const p: Profile = {
          id,
          name: String(params.name || "本地节点"),
          kind: "file",
          source: "本地配置",
          updated: new Date().toISOString(),
          active: shouldActivate,
          isPureLocal: true,
          isAggregate: false,
          hasPath: false,
          nodeCount: inputNodes.length,
          localNodeCount: 0,
          editedNodeCount: 0,
          editedGroupCount: 0,
          extraRuleCount: 0,
          templateCount: 0,
          dependentAggregates: [],
        };
        const st: FixtureProfileState = {
          profile: p,
          url: "",
          baseNodes: inputNodes.map(n => ({
            source: n.name,
            name: n.name,
            type: n.type,
            server: n.server,
            port: n.port,
            options: { ...(n.options ?? {}), type: n.type },
            edited: false,
            missing: false,
            base: { name: n.name, type: n.type, server: n.server, port: n.port, options: { ...(n.options ?? {}), type: n.type } },
          })),
          baseGroups: [{
            source: "Gopurr Select",
            name: "Gopurr Select",
            type: "select",
            members: [...inputNodes.map(n => n.name), "DIRECT"],
            inheritedMembers: [...inputNodes.map(n => n.name), "DIRECT"],
            appendMembers: [],
            explicit: false,
            explicitMembers: false,
            edited: false,
            missing: false,
            deleted: false,
            use: [],
            includeAll: false,
            base: { name: "Gopurr Select", type: "select", members: [...inputNodes.map(n => n.name), "DIRECT"] },
          }],
          edits: null,
        };
        if (shouldActivate) {
          profileStates.forEach(s => { s.profile.active = false; });
          status.activeProfile = id;
        }
        profileStates.push(st);
        if (shouldActivate) syncRuntimeGroupsFromActive();
        syncMeshSummary();
        return { profile: p, saved: true, applied: false, reconnectRequired: false, status };
      }
      case "profiles.import": {
        const first = profileStates.length === 0;
        const urlStr = params.url ? String(params.url) : "";
        let hostName = "example.invalid";
        if (urlStr) {
          try { hostName = new URL(urlStr).hostname || hostName; } catch { /* keep fallback */ }
        }
        const inferredName = String(params.name || "").trim() || (urlStr ? hostName : "本地配置");
        const profile: Profile = {
          id: first ? "imported" : `imported-${profileStates.length + 1}`,
          name: inferredName,
          kind: urlStr ? "url" : "file",
          source: urlStr ? hostName : "本地配置",
          updated: "2026-11-07T00:00:00Z",
          active: first,
          isPureLocal: !urlStr,
          userAgent: params.userAgent ? String(params.userAgent) : "",
          useProxy: !!params.useProxy,
          updateHours: typeof params.updateHours === "number" ? params.updateHours : null,
          nodeCount: 2,
          localNodeCount: 0,
          editedNodeCount: 0,
          editedGroupCount: 0,
          dependentAggregates: [],
        };
        profileStates.push(makeDefaultState(profile, urlStr));
        if (first) {
          status.activeProfile = profile.id;
          syncRuntimeGroupsFromActive();
        }
        return profile;
      }
      case "profiles.activate": {
        status.activeProfile = String(params.id);
        status.running = true;
        status.reconnectRequired = false;
        profileStates.forEach(s => { s.profile.active = s.profile.id === status.activeProfile; s.profile.reconnectRequired = false; });
        groups.forEach(g => g.members.forEach(m => { delete m.delay; }));
        syncRuntimeGroupsFromActive();
        groups.forEach(g => g.members.forEach(m => { delete m.delay; }));
        syncMeshSummary();
        return status;
      }
      case "engine.start":
        if (!status.activeProfile && !mesh.managed && !mesh.status.running) throw new Error("请先导入并选择配置");
        status.running = true; status.reconnectRequired = false; profileStates.forEach(s => { s.profile.reconnectRequired = false; }); syncMeshSummary(); return status;
      case "engine.stop": status.running = false; syncMeshSummary(); return status;
      case "engine.mode": status.mode = params.mode as Status["mode"]; syncMeshSummary(); return status;
      case "profiles.editor": {
        const st = profileStates.find(s => s.profile.id === params.id)!;
        const p = st.profile;
        return {
          id: p.id, name: p.name, kind: p.kind, url: st.url || (p.kind === "url" ? "https://example.invalid/profile.yaml" : ""),
          info: p.info, userAgent: p.userAgent ?? "", updateHours: p.updateHours ?? null, useProxy: !!p.useProxy,
          isAggregate: !!p.isAggregate, hasPath: !!p.hasPath, isPureLocal: !!p.isPureLocal,
          nodeCount: p.nodeCount ?? 2, localNodeCount: p.localNodeCount ?? 0, editedNodeCount: p.editedNodeCount ?? 0,
          editedGroupCount: p.editedGroupCount ?? 0, extraRuleCount: p.extraRuleCount ?? 0, templateCount: p.templateCount ?? 0,
          dependentAggregates: p.dependentAggregates ?? [], aggregate: p.aggregate,
        };
      }
      case "profiles.update": {
        const st = profileStates.find(s => s.profile.id === params.id)!;
        const p = st.profile;
        if (params.name) p.name = String(params.name);
        if (params.url !== undefined && p.kind === "url") st.url = String(params.url);
        if (params.userAgent !== undefined) p.userAgent = String(params.userAgent);
        if (params.useProxy !== undefined) p.useProxy = !!params.useProxy;
        if (params.clearUpdateHours) p.updateHours = null;
        else if (params.updateHours !== undefined) p.updateHours = params.updateHours === null ? null : Number(params.updateHours);
        return { profile: p, saved: true, applied: false, status };
      }
      case "profiles.refresh": {
        const st = profileStates.find(s => s.profile.id === params.id)!;
        const p = st.profile;
        p.reconnectRequired = p.active && status.running;
        status.reconnectRequired = !!p.reconnectRequired;
        return { profile: p, saved: true, applied: false, reconnectRequired: p.reconnectRequired, status };
      }
      case "profiles.delete": {
        const i = profileStates.findIndex(s => s.profile.id === params.id);
        if (i >= 0) profileStates.splice(i, 1);
        if (status.activeProfile === params.id) {
          status.activeProfile = profileStates[0]?.profile.id ?? "";
          profileStates.forEach(s => { s.profile.active = s.profile.id === status.activeProfile; });
        }
        return { saved: true, applied: false, status };
      }
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
