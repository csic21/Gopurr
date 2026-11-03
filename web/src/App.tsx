import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError, getSession, loadI18n, onUnauthorized, setCsrf } from "./api";
import { I18nProvider, useI18n } from "./i18n";
import { isManual, visibleGroups } from "./format";
import type {
  Connection,
  LogEntry,
  LogLevel,
  Mode,
  Page,
  ProxyGroup,
  Session,
  Status,
  Theme,
  ToastMessage,
  TrafficSample,
} from "./types";
import { Login } from "./components/Login";
import { Topbar } from "./components/Topbar";
import { Tabs } from "./components/Tabs";
import { Overview } from "./components/Overview";
import { Proxies } from "./components/Proxies";
import { Connections } from "./components/Connections";
import { Logs } from "./components/Logs";
import { Loading, Toast } from "./components/ui";

const THEME_KEY = "gopurr.web.theme";

function readTheme(): Theme {
  const saved = localStorage.getItem(THEME_KEY);
  return saved === "light" || saved === "dark" ? saved : "system";
}

export default function App() {
  const [dictionary, setDictionary] = useState<Record<string, string>>({});
  const [lang, setLang] = useState("en");
  const [session, setSession] = useState<Session | null>(null);
  const [ready, setReady] = useState(false);
  const [expired, setExpired] = useState(false);
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [systemDark, setSystemDark] = useState(() =>
    window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)").matches : false,
  );

  const dark = theme === "dark" || (theme === "system" && systemDark);

  useEffect(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const listener = (event: MediaQueryListEvent) => setSystemDark(event.matches);
    media.addEventListener("change", listener);
    return () => media.removeEventListener("change", listener);
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang.startsWith("zh") ? "zh-Hans" : "en";
    document.title = lang.startsWith("zh") ? "Gopurr 远程控制台" : "Gopurr Remote Console";
  }, [lang]);

  useEffect(() => {
    if (theme === "system") delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = theme;
    localStorage.setItem(THEME_KEY, theme);
  }, [theme]);

  useEffect(() => {
    onUnauthorized(() => {
      setSession(null);
      setCsrf("");
      setExpired(true);
    });
    return () => onUnauthorized(null);
  }, []);

  useEffect(() => {
    let alive = true;
    (async () => {
      const [dictionary, current] = await Promise.all([loadI18n(), getSession()]);
      if (!alive) return;
      setDictionary(dictionary);
      if (current) {
        setLang(current.lang);
        setCsrf(current.csrf);
        setSession(current);
      } else {
        setLang(navigator.language.toLowerCase().startsWith("zh") ? "zh-Hans" : "en");
      }
      setReady(true);
    })().catch(() => {
      if (alive) setReady(true);
    });
    return () => {
      alive = false;
    };
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((value) => (value === "dark" || (value === "system" && systemDark) ? "light" : "dark"));
  }, [systemDark]);

  const handleLogin = useCallback((current: Session) => {
    setCsrf(current.csrf);
    setLang(current.lang);
    setSession(current);
    setExpired(false);
  }, []);

  const handleLogout = useCallback(async () => {
    try {
      await api.logout();
    } catch {
      // The cookie may already be gone; the local screen still signs out.
    }
    setSession(null);
    setCsrf("");
    setExpired(false);
  }, []);

  if (!ready) {
    return (
      <I18nProvider lang={lang} dictionary={dictionary}>
        <div className="boot-screen">
          <img src="/assets/logo.svg" alt="Gopurr" />
          <Loading label="Gopurr" />
        </div>
      </I18nProvider>
    );
  }

  return (
    <I18nProvider lang={lang} dictionary={dictionary}>
      {session ? (
        <Console
          dark={dark}
          onToggleTheme={toggleTheme}
          onLogout={() => void handleLogout()}
        />
      ) : (
        <Login
          dark={dark}
          expired={expired}
          onToggleTheme={toggleTheme}
          onLogin={handleLogin}
        />
      )}
    </I18nProvider>
  );
}

function Console({
  dark,
  onToggleTheme,
  onLogout,
}: {
  dark: boolean;
  onToggleTheme: () => void;
  onLogout: () => void;
}) {
  const { t } = useI18n();
  const [page, setPage] = useState<Page>("overview");
  const [status, setStatus] = useState<Status | null>(null);
  const [groups, setGroups] = useState<ProxyGroup[]>([]);
  const [selectedGroup, setSelectedGroup] = useState("");
  const [connections, setConnections] = useState<Connection[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [logLevel, setLogLevel] = useState<LogLevel>("all");
  const [traffic, setTraffic] = useState<TrafficSample[]>([]);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState<ToastMessage | null>(null);
  const busyRef = useRef(false);
  const lastTrafficAt = useRef(0);
  const statusRef = useRef<Status | null>(null);

  const notify = useCallback((text: string, bad = false) => {
    setToast({ text, bad });
  }, []);

  const dismissToast = useCallback(() => setToast(null), []);

  const errorText = useCallback(
    (error: unknown) => {
      if (error instanceof ApiError) {
        if (error.status === 401) return t("会话已过期，请重新登录");
        if (error.status === 429) return t("登录尝试过多，请稍后再试");
        return error.message || t("未知错误");
      }
      return t("网络请求失败");
    },
    [t],
  );

  const applyStatus = useCallback((next: Status) => {
    statusRef.current = next;
    setStatus(next);
    if (next.traffic.at && next.traffic.at !== lastTrafficAt.current) {
      lastTrafficAt.current = next.traffic.at;
      setTraffic((previous) => [
        ...previous.slice(-71),
        { down: next.traffic.down, up: next.traffic.up },
      ]);
    }
  }, []);

  const applyGroups = useCallback((next: ProxyGroup[], mode: Mode) => {
    setGroups(next);
    setSelectedGroup((current) => {
      const visible = visibleGroups(next, mode);
      if (visible.some((group) => group.name === current)) return current;
      return (visible.find(isManual) ?? visible[0])?.name ?? "";
    });
  }, []);

  const refreshStatus = useCallback(async () => {
    applyStatus(await api.status());
  }, [applyStatus]);

  const refreshGroups = useCallback(async () => {
    const data = await api.groups();
    applyGroups(data.groups, statusRef.current?.mode ?? "rule");
  }, [applyGroups]);

  const refreshConnections = useCallback(async () => {
    const data = await api.connections();
    setConnections(data.connections);
  }, []);

  const refreshLogs = useCallback(async (level: LogLevel) => {
    const data = await api.logs(level);
    setLogs(data.logs);
  }, []);

  const handleRefresh = useCallback(async () => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    try {
      await refreshStatus();
      await refreshGroups();
      if (page === "connections") await refreshConnections();
      if (page === "logs") await refreshLogs(logLevel);
    } catch (error) {
      notify(errorText(error), true);
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [errorText, logLevel, notify, page, refreshConnections, refreshGroups, refreshLogs, refreshStatus]);

  useEffect(() => {
    let cancelled = false;
    let ticking = false;
    const tick = async () => {
      if (cancelled || document.hidden || ticking) return;
      ticking = true;
      try {
        applyStatus(await api.status());
        const data = await api.groups();
        if (cancelled) return;
        applyGroups(data.groups, statusRef.current?.mode ?? "rule");
        if (page === "connections") {
          const current = await api.connections();
          if (!cancelled) setConnections(current.connections);
        }
        if (page === "logs") {
          const current = await api.logs(logLevel);
          if (!cancelled) setLogs(current.logs);
        }
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) return;
      } finally {
        ticking = false;
      }
    };
    void tick();
    const timer = window.setInterval(() => void tick(), 2000);
    const onVisible = () => {
      if (!document.hidden) void tick();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [page, logLevel, applyStatus, applyGroups]);

  const mutate = useCallback(
    async (action: () => Promise<unknown>, success?: string) => {
      if (busyRef.current) return;
      busyRef.current = true;
      setBusy(true);
      try {
        await action();
        applyStatus(await api.status());
        const data = await api.groups();
        applyGroups(data.groups, statusRef.current?.mode ?? "rule");
        if (page === "connections") {
          const current = await api.connections();
          setConnections(current.connections);
        }
        notify(success || t("操作成功"));
      } catch (error) {
        notify(errorText(error), true);
        try {
          applyStatus(await api.status());
          const data = await api.groups();
          applyGroups(data.groups, statusRef.current?.mode ?? "rule");
        } catch {
          // The next poll retries.
        }
      } finally {
        busyRef.current = false;
        setBusy(false);
      }
    },
    [applyGroups, applyStatus, errorText, notify, page, t],
  );

  const handleMode = useCallback(
    (mode: Mode) => {
      if (busyRef.current) return;
      setStatus((current) => (current ? { ...current, mode } : current));
      void mutate(() => api.mode(mode));
    },
    [mutate],
  );

  const handleSelect = useCallback(
    async (group: string, member: string) => {
      if (busyRef.current) return;
      setGroups((current) =>
        current.map((item) => (item.name === group ? { ...item, now: member } : item)),
      );
      await mutate(() => api.select(group, member), t("操作成功"));
    },
    [mutate, t],
  );

  const handleSystemProxy = useCallback(
    (on: boolean) => {
      if (busyRef.current) return;
      setStatus((current) => (current ? { ...current, systemProxy: on } : current));
      void mutate(() => api.systemProxy(on));
    },
    [mutate],
  );

  const handleTun = useCallback(
    (on: boolean) => {
      if (busyRef.current) return;
      setStatus((current) => (current ? { ...current, tun: on } : current));
      void mutate(() => api.tun(on));
    },
    [mutate],
  );

  const handleDelay = useCallback(
    async (group: string, names: string[]) => {
      try {
        await api.delay(group, names);
        const data = await api.groups();
        applyGroups(data.groups, statusRef.current?.mode ?? "rule");
        notify(t("测试完成"));
      } catch (error) {
        notify(errorText(error), true);
      }
    },
    [applyGroups, errorText, notify, t],
  );

  const handleDisconnect = useCallback(
    (id: string) => mutate(() => api.closeConnection(id), t("已断开")),
    [mutate, t],
  );

  const handleDisconnectAll = useCallback(
    () => mutate(() => api.closeAllConnections(), t("已断开")),
    [mutate, t],
  );

  const handleClearLogs = useCallback(async () => {
    try {
      await api.clearLogs();
      setLogs([]);
      notify(t("操作成功"));
    } catch (error) {
      notify(errorText(error), true);
    }
  }, [errorText, notify, t]);

  const visible = useMemo(
    () => visibleGroups(groups, status?.mode ?? "rule"),
    [groups, status?.mode],
  );
  const group = useMemo(
    () => visible.find((item) => item.name === selectedGroup) ?? visible.find(isManual) ?? visible[0] ?? null,
    [selectedGroup, visible],
  );

  return (
    <div className="app">
      <Topbar
        status={status}
        dark={dark}
        onToggleTheme={onToggleTheme}
        onRefresh={() => void handleRefresh()}
        onLogout={onLogout}
        busy={busy}
      />
      <Tabs page={page} onChange={setPage} />
      <main className="main">
        {page === "overview" ? (
          status ? (
            <Overview
              status={status}
              groups={groups}
              group={group}
              traffic={traffic}
              dark={dark}
              busy={busy}
              onMode={handleMode}
              onSystemProxy={handleSystemProxy}
              onTun={handleTun}
            />
          ) : (
            <Loading label={t("加载中…")} />
          )
        ) : null}

        {page === "proxies" ? (
          status ? (
            <Proxies
              status={status}
              groups={groups}
              group={group}
              busy={busy}
              onSelectGroup={setSelectedGroup}
              onMode={handleMode}
              onSelect={handleSelect}
              onDelay={handleDelay}
            />
          ) : (
            <Loading label={t("加载中…")} />
          )
        ) : null}

        {page === "connections" ? (
          <Connections
            connections={connections}
            busy={busy}
            onDisconnect={handleDisconnect}
            onDisconnectAll={handleDisconnectAll}
          />
        ) : null}

        {page === "logs" ? (
          <Logs
            logs={logs}
            level={logLevel}
            onLevelChange={setLogLevel}
            onClear={handleClearLogs}
            busy={busy}
          />
        ) : null}
      </main>
      <Toast toast={toast} onDismiss={dismissToast} />
    </div>
  );
}
