import { useState } from "react";
import { useI18n } from "../i18n";
import { formatBytes, formatTime } from "../format";
import type { Connection } from "../types";
import { Empty, Modal, SearchBox } from "./ui";

export function Connections({
  connections,
  busy,
  onDisconnect,
  onDisconnectAll,
}: {
  connections: Connection[];
  busy: boolean;
  onDisconnect: (id: string) => Promise<void>;
  onDisconnectAll: () => Promise<void>;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Connection | null>(null);
  const [confirmAll, setConfirmAll] = useState(false);
  const filtered = connections.filter((connection) => {
    const value = query.trim().toLowerCase();
    if (!value) return true;
    return [
      connection.host,
      connection.process,
      connection.processPath,
      connection.rule,
      connection.rulePayload,
      connection.chain,
      connection.destination,
    ].some((item) => String(item || "").toLowerCase().includes(value));
  });

  async function disconnect(id: string) {
    setSelected(null);
    await onDisconnect(id);
  }

  async function disconnectAll() {
    setConfirmAll(false);
    await onDisconnectAll();
  }

  return (
    <section className="page">
      <div className="page-head">
        <div>
          <h1>{t("连接")}</h1>
          <p className="muted small">
            {t("活跃连接")} {filtered.length}
          </p>
        </div>
        <div className="page-actions">
          <SearchBox
            value={query}
            onChange={setQuery}
            placeholder={t("搜索主机、进程或规则…")}
            className="grow"
          />
          <button
            type="button"
            className="btn danger"
            disabled={busy || connections.length === 0}
            onClick={() => setConfirmAll(true)}
          >
            {t("断开全部")}
          </button>
        </div>
      </div>

      <div className="conn-list card">
        {filtered.length === 0 ? (
          <Empty>{query.trim() ? t("没有匹配的连接") : t("暂无连接")}</Empty>
        ) : (
          filtered.map((connection) => (
            <div className="conn-row" key={connection.id}>
              <div className="conn-host">
                <strong title={connection.host}>{connection.host || connection.destination || "—"}</strong>
                <span title={connection.processPath || connection.process || ""}>
                  {connection.process || connection.network || ""}
                  {connection.rulePayload ? ` · ${connection.rulePayload}` : connection.rule ? ` · ${connection.rule}` : ""}
                </span>
              </div>
              <span className="conn-chain" title={(connection.chains || []).join(" » ")}>
                {connection.chain || "—"}
              </span>
              <span className="conn-rate">
                ↑ {formatBytes(connection.up)}
                <br />↓ {formatBytes(connection.down)}
              </span>
              <button type="button" className="btn conn-open" onClick={() => setSelected(connection)}>
                {t("连接详情")}
              </button>
            </div>
          ))
        )}
      </div>

      {selected ? (
        <Modal
          title={t("连接详情")}
          onClose={() => setSelected(null)}
          footer={
            <>
              <button type="button" className="btn" onClick={() => setSelected(null)}>
                {t("关闭")}
              </button>
              <button type="button" className="btn danger" onClick={() => void disconnect(selected.id)}>
                {t("断开")}
              </button>
            </>
          }
        >
          <dl className="detail-grid">
            {detailRows(selected, t).map(([label, value]) => (
              <div key={label} className="detail-row">
                <dt>{label}</dt>
                <dd>{value}</dd>
              </div>
            ))}
          </dl>
        </Modal>
      ) : null}

      {confirmAll ? (
        <Modal
          title={t("断开全部连接？")}
          onClose={() => setConfirmAll(false)}
          footer={
            <>
              <button type="button" className="btn" onClick={() => setConfirmAll(false)}>
                {t("取消")}
              </button>
              <button type="button" className="btn danger" onClick={() => void disconnectAll()}>
                {t("断开全部")}
              </button>
            </>
          }
        >
          <p className="muted">{t("当前会话会中断，应用通常会自动重新连接。")}</p>
        </Modal>
      ) : null}
    </section>
  );
}

function detailRows(connection: Connection, t: (key: string) => string): Array<[string, string]> {
  const rows: Array<[string, string | undefined]> = [
    [t("主机"), connection.host],
    [t("进程"), connection.process],
    [t("进程路径"), connection.processPath],
    [t("源地址"), connection.source],
    [t("目标地址"), connection.destination],
    [t("命中规则"), [connection.rule, connection.rulePayload].filter(Boolean).join(" · ")],
    [t("链路"), (connection.chains || []).join(" » ") || connection.chain],
    [t("上传"), formatBytes(connection.up)],
    [t("下载"), formatBytes(connection.down)],
    [t("开始时间"), formatTime(connection.start)],
  ];
  return rows.filter((row): row is [string, string] => Boolean(row[1]));
}
