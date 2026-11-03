import { useEffect, useRef, useState } from "react";
import { useI18n } from "../i18n";
import { formatTime } from "../format";
import type { LogEntry, LogLevel } from "../types";
import { Empty, SearchBox, Segmented } from "./ui";

export function Logs({
  logs,
  level,
  onLevelChange,
  onClear,
  busy,
}: {
  logs: LogEntry[];
  level: LogLevel;
  onLevelChange: (level: LogLevel) => void;
  onClear: () => Promise<void>;
  busy: boolean;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [paused, setPaused] = useState(false);
  const [frozen, setFrozen] = useState<LogEntry[]>([]);
  const [autoScroll, setAutoScroll] = useState(true);
  const listRef = useRef<HTMLDivElement | null>(null);
  const logsRef = useRef(logs);

  useEffect(() => {
    logsRef.current = logs;
  }, [logs]);

  useEffect(() => {
    if (paused) setFrozen(logsRef.current);
  }, [paused]);

  const source = paused ? frozen : logs;
  const shown = source.filter((entry) => {
    const value = query.trim().toLowerCase();
    if (!value) return true;
    return entry.text.toLowerCase().includes(value) || entry.level.toLowerCase().includes(value);
  });

  useEffect(() => {
    const element = listRef.current;
    if (!element || !autoScroll) return;
    element.scrollTop = element.scrollHeight;
  }, [shown.length, autoScroll, paused]);

  function onScroll() {
    const element = listRef.current;
    if (!element) return;
    const nearBottom = element.scrollHeight - element.scrollTop - element.clientHeight < 80;
    setAutoScroll(nearBottom);
  }

  async function clear() {
    await onClear();
    setFrozen([]);
    setPaused(false);
  }

  return (
    <section className="page">
      <div className="page-head">
        <div>
          <h1>{t("日志")}</h1>
          <p className="muted small">
            {t("日志")} · {logs.length}
          </p>
        </div>
        <div className="page-actions">
          <Segmented<LogLevel>
            value={level}
            small
            label={t("日志级别")}
            onChange={onLevelChange}
            options={[
              { value: "all", label: t("全部") },
              { value: "info", label: t("信息") },
              { value: "warning", label: t("警告") },
              { value: "error", label: t("错误") },
            ]}
          />
          <button
            type="button"
            className={`btn ${paused ? "primary" : ""}`}
            onClick={() => setPaused((value) => !value)}
          >
            {paused ? t("继续") : t("暂停")}
          </button>
          <button type="button" className="btn" disabled={busy} onClick={() => void clear()}>
            {t("清空")}
          </button>
        </div>
      </div>

      <div className="log-toolbar">
        <SearchBox
          value={query}
          onChange={setQuery}
          placeholder={t("搜索日志…")}
          className="grow"
        />
        {paused ? <span className="pill warn">{t("已暂停")}</span> : null}
        <span className="muted tiny">
          {shown.length} / {source.length}
        </span>
      </div>

      <div className="log-list card" role="log" aria-live="polite" ref={listRef} onScroll={onScroll}>
        {shown.length === 0 ? (
          <Empty>{t("暂无日志")}</Empty>
        ) : (
          shown.map((entry, index) => (
            <div className="log-row" key={`${entry.at}-${index}`}>
              <span className="log-time">{formatTime(entry.at)}</span>
              <span className={`log-level ${entry.level.toLowerCase()}`}>
                {entry.level.toUpperCase()}
              </span>
              <span className="log-text">{entry.text}</span>
            </div>
          ))
        )}
      </div>

      {!autoScroll ? (
        <button
          type="button"
          className="floating"
          onClick={() => {
            setAutoScroll(true);
            const element = listRef.current;
            if (element) element.scrollTop = element.scrollHeight;
          }}
        >
          {t("回到底部")}
        </button>
      ) : null}
    </section>
  );
}
