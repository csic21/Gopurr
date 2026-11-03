import { ArrowDown, ArrowUp } from "lucide-react";
import { useI18n } from "../i18n";
import { formatBytes, formatRate, resolveExit, typeLabel } from "../format";
import type { Mode, ProxyGroup, Status, TrafficSample } from "../types";
import { Segmented, Switch } from "./ui";
import { TrafficChart } from "./TrafficChart";

export function Overview({
  status,
  groups,
  group,
  traffic,
  dark,
  busy,
  onMode,
  onSystemProxy,
  onTun,
}: {
  status: Status;
  groups: ProxyGroup[];
  group: ProxyGroup | null;
  traffic: TrafficSample[];
  dark: boolean;
  busy: boolean;
  onMode: (mode: Mode) => void;
  onSystemProxy: (on: boolean) => void;
  onTun: (on: boolean) => void;
}) {
  const { t } = useI18n();
  const mode = status.mode;
  const direct = mode === "direct";
  const exitName = direct ? t("直连") : group ? resolveExit(groups, group) || t("未选择节点") : t("暂无策略组");
  const exitMeta = direct
    ? t("全部连接直接访问网络")
    : group
      ? `${group.name} · ${typeLabel(group.type, t)}`
      : t("未选择节点");
  const stale = !status.traffic || status.traffic.stale;
  const sysStatus = !status.systemProxyAvailable
    ? t("系统代理需要在本机授权")
    : status.systemProxy
      ? t("系统代理已开启")
      : t("系统代理已关闭");
  let tunStatus = status.tun ? t("TUN 已开启") : t("TUN 已关闭");
  if (!status.tun && !status.tunInstalled) tunStatus = t("TUN 需要先安装系统服务");
  else if (!status.tun && !status.tunAvailable) tunStatus = t("需要系统服务");
  const disabled = busy || status.busy;
  const message = status.error || status.notice;

  return (
    <section className="page">
      <div className="page-head">
        <div>
          <h1>{t("概览")}</h1>
          <p className="muted small">{t("连接状态与实时网络活动")}</p>
        </div>
      </div>

      {message ? (
        <div className={`banner ${status.error ? "bad" : ""}`} role="status">
          {message}
        </div>
      ) : null}

      <div className="hero card">
        <div className="hero-main">
          <div>
            <p className="eyebrow">{t("路由模式")}</p>
            <Segmented<Mode>
              value={mode}
              label={t("路由模式")}
              onChange={onMode}
              disabled={disabled || !status.kernel}
              options={[
                { value: "rule", label: t("规则") },
                { value: "global", label: t("全局") },
                { value: "direct", label: t("直连") },
              ]}
            />
          </div>
          <div className="exit-block">
            <p className="eyebrow">{t("当前出口")}</p>
            <h2 className={`exit-name ${exitName === t("未选择节点") || exitName === t("暂无策略组") ? "empty" : ""}`}>
              {exitName}
            </h2>
            <p className="muted small">{exitMeta}</p>
          </div>
        </div>
        <div className="hero-side">
          <div className="rate-row">
            <div className="rate down">
              <span className="rate-icon">
                <ArrowDown className="icon" aria-hidden="true" />
              </span>
              <div>
                <span className="rate-label">{t("下行")}</span>
                <strong>{stale ? "—" : formatRate(status.traffic.down)}</strong>
              </div>
            </div>
            <div className="rate up">
              <span className="rate-icon">
                <ArrowUp className="icon" aria-hidden="true" />
              </span>
              <div>
                <span className="rate-label">{t("上行")}</span>
                <strong>{stale ? "—" : formatRate(status.traffic.up)}</strong>
              </div>
            </div>
          </div>
          <TrafficChart samples={traffic} dark={dark} />
          <p id="traffic-note" className="muted tiny">
            {stale ? t("数据已暂停") : `${t("实时流量")} · ${t("现在")}`}
          </p>
        </div>
      </div>

      <div className="control-list card">
        <div className="control-row">
          <div className="control-text">
            <strong>{t("系统代理")}</strong>
            <span className={`muted small ${status.systemProxy ? "ok-text" : ""}`}>{sysStatus}</span>
          </div>
          <Switch
            checked={status.systemProxy}
            disabled={disabled || !status.systemProxyAvailable}
            label={t("系统代理")}
            onChange={onSystemProxy}
          />
        </div>
        <div className="control-row">
          <div className="control-text">
            <strong>{t("TUN 模式")}</strong>
            <span className="muted small">{tunStatus}</span>
          </div>
          <Switch
            checked={status.tun}
            disabled={disabled || (!status.tun && !status.tunAvailable)}
            label={t("TUN 模式")}
            onChange={onTun}
          />
        </div>
      </div>

      <div className="stats">
        <div className="stat card">
          <span className="muted tiny">{t("活跃连接")}</span>
          <strong>{status.kernel && !status.connectionsStale ? status.connections : "—"}</strong>
        </div>
        <div className="stat card">
          <span className="muted tiny">{t("生效规则")}</span>
          <strong>{status.kernel ? status.rules : "—"}</strong>
        </div>
        <div className="stat card">
          <span className="muted tiny">{t("累计下行")}</span>
          <strong>{formatBytes(status.traffic.downTotal)}</strong>
        </div>
        <div className="stat card">
          <span className="muted tiny">{t("累计上行")}</span>
          <strong>{formatBytes(status.traffic.upTotal)}</strong>
        </div>
      </div>
    </section>
  );
}
