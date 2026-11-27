import type { DesktopSnapshot } from "./types";

export function desktopFixtureTransport(language: "zh-Hans" | "en" = "zh-Hans") {
  const nodes = [
    { name: "香港 01", type: "Shadowsocks", delay: 42, delayKnown: true },
    { name: "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", type: "Vmess", delay: 86, delayKnown: true },
    { name: "新加坡 03", type: "Trojan", delay: 58, delayKnown: true },
    { name: "自建 VPS 节点", type: "VLESS", delay: 31, delayKnown: true },
    { name: "DIRECT", type: "Direct", delay: 0, delayKnown: false },
  ];
  const state: DesktopSnapshot = {
    apiVersion: 2,
    status: {
      version: "0.6.20",
      kernel: true,
      mode: "rule",
      systemProxy: true,
      systemProxyAvailable: true,
      tun: false,
      tunAvailable: false,
      tunInstalled: false,
      traffic: { up: 19456, down: 921600, upTotal: 63963136, downTotal: 851443712 },
      connections: 4,
      rules: 4,
      proxies: 197,
    },
    prefs: {
      language,
      sidebarCollapsed: false,
      autoUpdateHours: 12,
      shortcuts: ["mod+shift+p", "mod+shift+t", "mod+shift+g", "mod+shift+l", "mod+shift+r", "mod+shift+c"],
      closeOnSwitch: true,
      checkAppUpdates: true,
      testURL: "https://www.gstatic.com/generate_204",
      testTimeoutMS: 6000,
      testConcurrency: 8,
    },
    network: { mixedPort: 7890, allowLan: false, ipv6: true, tunStack: "mixed", tunAutoRoute: true },
    features: {
      dns: { enable: true, enhancedMode: "fake-ip", nameserver: ["1.1.1.1", "223.5.5.5"], defaultNameserver: ["1.1.1.1", "223.5.5.5"] },
      sniffer: { enable: true, forceDNSMapping: true, parsePureIP: true, httpPorts: ["80", "8080-8880"], tlsPorts: ["443", "8443"], quicPorts: ["443"] },
      dnsPolicy: null,
      snifferPolicy: null,
    },
    profiles: [
      {
        id: "daily",
        name: "日常订阅 / Everyday",
        kind: "url",
        source: "https://example.com/config",
        updated: new Date(Date.now() - 2 * 3600 * 1000).toISOString(),
        info: { upload: 12884901888, download: 32856500000, total: 107374182400, expire: Math.floor(Date.now() / 1000) + 23 * 86400 },
        updateHours: null,
        useProxy: false,
        userAgent: "",
        hasPath: false,
        isPureLocal: false,
        nodeCount: 48,
        localNodeCount: 1,
        editedNodeCount: 1,
        editedGroupCount: 1,
        extraRuleCount: 2,
        templateCount: 1,
        active: true,
      },
      {
        id: "backup",
        name: "备用配置 · A deliberately long profile name for desktop verification",
        kind: "file",
        source: "/tmp/backup.yaml",
        updated: new Date(Date.now() - 26 * 3600 * 1000).toISOString(),
        info: {},
        hasPath: true,
        isPureLocal: false,
        nodeCount: 16,
        localNodeCount: 0,
        editedNodeCount: 1,
        editedGroupCount: 0,
        extraRuleCount: 0,
        templateCount: 0,
        active: false,
      },
      {
        id: "local-vps",
        name: "自建 VPS 合集",
        kind: "file",
        source: "",
        updated: new Date(Date.now() - 5 * 3600 * 1000).toISOString(),
        info: {},
        hasPath: false,
        isPureLocal: true,
        nodeCount: 3,
        localNodeCount: 0,
        editedNodeCount: 0,
        editedGroupCount: 0,
        extraRuleCount: 0,
        templateCount: 0,
        active: false,
      },
    ],
    activeProfile: "daily",
    groups: [
      { name: "节点选择", type: "Selector", now: "香港 01", hidden: false, members: nodes },
      { name: "自动选择", type: "URLTest", now: "新加坡 03", members: nodes.slice(0, 4) },
      { name: "GLOBAL", type: "Selector", now: "节点选择", members: [{ name: "节点选择", type: "Selector", delayKnown: false }] },
    ],
    groupsPreview: false,
    history: Array.from({ length: 72 }, (_, i) => ({
      down: Math.round(70000 + Math.pow(Math.sin(i * 0.19), 8) * 1500000 + Math.pow(Math.sin(i * 0.7), 4) * 200000),
      up: Math.round(12000 + Math.pow(Math.cos(i * 0.23), 6) * 70000),
    })),
    service: { installed: false, busy: false },
    capabilities: { systemProxy: true, tun: false, desktop: true },
    connections: Array.from({ length: 4 }, (_, i) => ({
      id: `conn-${i}`,
      host: ["api.github.com", "www.youtube.com", "a-very-long-hostname.for-desktop-layout.example.com", "download.example.com"][i],
      process: ["Gopurr", "Safari", "curl", "Telegram"][i],
      processPath: "/Applications/Example.app/Contents/MacOS/Example",
      network: "TCP",
      source: "127.0.0.1:51200",
      destination: "104.21.72.23:443",
      rule: "DOMAIN-SUFFIX",
      rulePayload: "github.com",
      chain: "节点选择 → 香港 01",
      chains: ["节点选择", "香港 01"],
      up: 12345,
      down: 950123,
      start: "2026-11-07T08:35:00Z",
    })),
    effectiveRules: [
      { type: "DOMAIN-SUFFIX", payload: "github.com", adapter: "节点选择" },
      { type: "GEOIP", payload: "CN", adapter: "DIRECT" },
      { type: "DOMAIN-SUFFIX", payload: "example.com", adapter: "REJECT" },
      { type: "MATCH", payload: "", adapter: "节点选择" },
    ],
    logs: Array.from({ length: 45 }, (_, i) => ({
      at: new Date(1794038400000 + i * 1000).toISOString(),
      level: i % 7 === 0 ? "warning" : "info",
      text: i % 7 === 0
        ? "[DNS] upstream lookup timed out; falling back to secondary nameserver"
        : "[TCP] 127.0.0.1:51200 --> api.github.com:443 match DomainSuffix(github.com) using 节点选择[香港 01]",
    })),
    scope: { id: "", extraRules: [], mergeYaml: "", dns: null, templateIds: [] },
    library: [],
    appRules: [],
    mesh: {
      status: {
        running: true,
        shieldsKnown: true,
        shieldsUp: true,
        tailnet: "example.ts.net",
        self: { id: "self", name: "MacBook Pro", os: "macOS", online: true, ips: ["100.64.0.1"] },
        peers: [
          { id: "peer-1", name: "Android Phone", os: "android", online: true, ips: ["100.64.0.2"], owner: "user@example.com" },
          { id: "peer-2", name: "Home Server", os: "linux", online: false, ips: ["100.64.0.3"], owner: "user@example.com" },
        ],
        services: [],
        blocked: [],
      },
      visits: [],
      busy: false,
      autoConnect: false,
    },
    meshShortcuts: {},
    meshAccess: { ready: true },
    update: {},
  };

  let editorEdits: any = {
    nodes: [
      { source: "香港 01", server: "hk1.example.com" },
      { source: "", localId: "vps-1", name: "自建 VPS 节点", server: "vps.example.org", port: 443, options: { type: "vless", uuid: "11111111-2222-3333-4444-555555555555", tls: true } },
    ],
    groups: [
      { source: "节点选择", appendMembers: ["自建 VPS 节点"] },
    ],
  };

  const buildEditorModel = (id: string) => {
    const profile = state.profiles.find((p: any) => p.id === id) ?? state.profiles[0];
    const baseNodes = [
      { source: "香港 01", name: "香港 01", type: "ss", server: "hk1.example.com", port: 443, edited: true, missing: false, options: { type: "ss", cipher: "aes-128-gcm", password: "secret-password" }, base: { name: "香港 01", type: "ss", server: "server.example.com", port: 443, options: { type: "ss", cipher: "aes-128-gcm", password: "secret-password" } } },
      { source: "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", name: "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", type: "vmess", server: "jp.example.com", port: 443, edited: false, missing: false, options: { type: "vmess", uuid: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", cipher: "auto", alterId: 0, tls: true }, base: { name: "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", type: "vmess", server: "jp.example.com", port: 443, options: { type: "vmess", uuid: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", cipher: "auto", alterId: 0, tls: true } } },
      { source: "新加坡 03", name: "新加坡 03", type: "trojan", server: "sg.example.com", port: 443, edited: false, missing: false, options: { type: "trojan", password: "trojan-password", sni: "sg.example.com" }, base: { name: "新加坡 03", type: "trojan", server: "sg.example.com", port: 443, options: { type: "trojan", password: "trojan-password", sni: "sg.example.com" } } },
      { source: "", localId: "vps-1", name: "自建 VPS 节点", type: "vless", server: "vps.example.org", port: 443, edited: true, missing: false, options: { type: "vless", uuid: "11111111-2222-3333-4444-555555555555", tls: true, servername: "vps.example.org" }, base: null },
    ];
    return {
      id: profile?.id ?? "daily",
      profileId: profile?.id ?? "daily",
      profileName: profile?.name ?? "日常订阅 / Everyday",
      profileKind: profile?.kind ?? "url",
      isPureLocal: !!profile?.isPureLocal,
      edits: editorEdits,
      nodes: baseNodes,
      groups: [
        {
          source: "节点选择",
          name: "节点选择",
          type: "select",
          members: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03", "自建 VPS 节点", "DIRECT"],
          inheritedMembers: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03", "DIRECT"],
          appendMembers: ["自建 VPS 节点"],
          explicit: false,
          explicitMembers: false,
          edited: true,
          missing: false,
          deleted: false,
          base: { name: "节点选择", type: "select", members: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03", "DIRECT"] },
        },
        {
          source: "自动选择",
          name: "自动选择",
          type: "url-test",
          members: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03"],
          inheritedMembers: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03"],
          appendMembers: [],
          explicit: false,
          explicitMembers: false,
          edited: false,
          missing: false,
          deleted: false,
          base: { name: "自动选择", type: "url-test", members: ["香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03"] },
        },
      ],
      options: ["DIRECT", "REJECT", "香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03", "自建 VPS 节点", "节点选择", "自动选择"],
      availableNames: ["DIRECT", "REJECT", "香港 01", "日本 Tokyo / 长名称节点用于验证自动省略和选中状态", "新加坡 03", "自建 VPS 节点", "节点选择", "自动选择"],
      providerNodes: [],
    };
  };

  return async (method: string, params: any = {}) => {
    if (method === "desktop.profiles.import" && params.url?.includes("invalid")) {
      throw new Error("订阅下载失败，请检查链接后重试。 / Subscription download failed. Check the URL and retry.");
    }
    if (method === "desktop.snapshot") return state;
    if (method === "desktop.page.set") return {};
    if (method === "desktop.prefs.save") { Object.assign(state.prefs, params.prefs ?? params); return {}; }
    if (method === "desktop.mode.set") { state.status.mode = params.mode; return {}; }
    if (method === "desktop.systemProxy.set") { state.status.systemProxy = params.enabled; return {}; }
    if (method === "desktop.features.policy") { state.features[params.kind + "Policy"] = params.enabled; return {}; }
    if (method === "desktop.kernel.toggle") { state.status.kernel = !state.status.kernel; return {}; }
    if (method === "desktop.connections.close") { state.connections = state.connections.filter((c: any) => c.id !== params.id); return {}; }
    if (method === "desktop.connections.closeAll") { state.connections = []; return {}; }
    if (method === "desktop.logs.clear") { state.logs = []; return {}; }
    if (method === "desktop.group.select") {
      const g = state.groups.find((v: any) => v.name === params.group);
      if (g) g.now = params.member;
      return {};
    }
    if (method === "desktop.node.delay") return { delay: 35 };
    if (method === "desktop.profiles.get") {
      const found = state.profiles.find((v: any) => v.id === params.id);
      return { ...found, url: found?.kind === "url" ? "https://example.com/config" : "", path: found?.kind === "file" ? (found?.source ?? "") : "", content: "proxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n" };
    }
    if (method === "desktop.profiles.updateAll") return {};
    if (method === "desktop.profiles.update") return {};
    if (method === "desktop.profiles.createLocal") {
      const created = {
        id: "local-" + Date.now(),
        name: params.name || "本地节点",
        kind: "file",
        source: "",
        hasPath: false,
        isPureLocal: true,
        updated: new Date().toISOString(),
        info: {},
        nodeCount: (params.nodes ?? []).length,
        localNodeCount: 0,
        editedNodeCount: 0,
        editedGroupCount: 0,
        active: !!params.activate,
      };
      if (params.activate) {
        for (const pr of state.profiles) pr.active = false;
        state.activeProfile = created.id;
      }
      state.profiles.push(created);
      return { profile: created, active: !!params.activate };
    }
    if (method === "desktop.profiles.aggregatePreview") {
      const spec = params.spec ?? {};
      const srcs = spec.sources ?? [];
      const names = srcs.flatMap((s: any) => [`${s.prefix ?? ""}香港 01`, `${s.prefix ?? ""}日本 02`, `${s.prefix ?? ""}新加坡 03`]);
      const inc = spec.include ? new RegExp(spec.include, "i") : null;
      const exc = spec.exclude ? new RegExp(spec.exclude, "i") : null;
      const matched = names.filter((n: string) => (!inc || inc.test(n)) && (!exc || !exc.test(n)));
      const output = spec.dedupe ? [...new Set(matched)] : matched;
      return {
        ...(params.includeYaml === false ? {} : { yaml: "proxies: []\n" }),
        stats: { input: names.length, matched: matched.length, output: output.length, duplicates: matched.length - output.length },
        names: output,
        truncated: false,
      };
    }
    if (method === "desktop.proxies.parse") {
      const text = String(params.text ?? "").trim();
      const mode = params.mode ?? "auto";
      if (!text) return { kind: "empty", nodes: [], failures: [] };
      if (/^https?:\/\/\S+$/i.test(text) && !text.includes("\n")) {
        if (mode === "nodes") {
          return { kind: "nodes", ambiguousHttp: true, nodes: [{ name: "HTTP-Proxy", type: "http", server: "proxy.example.com", port: 8080, options: { type: "http" } }], failures: [] };
        }
        return { kind: "url", url: text, ambiguousHttp: true, nodes: [], failures: [] };
      }
      const lines = text.split(/\r?\n/).map((s: string) => s.trim()).filter(Boolean);
      const parsed: any[] = [];
      const failures: any[] = [];
      lines.forEach((line: string, i: number) => {
        if (/^(vless|vmess|trojan|ss|hy2|hysteria2|tuic|wireguard|socks5|http):\/\//i.test(line)) {
          const proto = line.split("://")[0].toLowerCase().replace("hy2", "hysteria2");
          const hash = line.includes("#") ? decodeURIComponent(line.split("#")[1]) : `${proto.toUpperCase()}-${i + 1}`;
          parsed.push({ name: hash, type: proto, server: "vps.example.org", port: 443, options: { type: proto, tls: true } });
        } else {
          failures.push({ line: i + 1, raw: line, reason: "unsupported URI scheme" });
        }
      });
      if (parsed.length || failures.length) return { kind: "nodes", nodes: parsed, failures };
      return { kind: "unknown", nodes: [], failures: [] };
    }
    if (method === "desktop.proxies.editor") return buildEditorModel(params.id || state.activeProfile);
    if (method === "desktop.proxies.save") {
      editorEdits = params.edits ?? { nodes: [], groups: [] };
      return { saved: true, applied: true };
    }
    if (method.startsWith("desktop.file.")) return { path: null };
    return {};
  };
}
