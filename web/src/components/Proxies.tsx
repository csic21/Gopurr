import { useState } from "react";
import { Check, Play } from "lucide-react";
import { useI18n } from "../i18n";
import {
  isManual,
  latencyClass,
  latencyText,
  typeLabel,
  uniqueNodeCount,
  visibleGroups,
} from "../format";
import type { Member, Mode, ProxyGroup, Status } from "../types";
import { Empty, SearchBox, Segmented } from "./ui";

const builtin = new Set(["DIRECT", "REJECT", "REJECT-DROP", "PASS", "COMPATIBLE"]);

export function Proxies({
  status,
  groups,
  group,
  busy,
  onSelectGroup,
  onMode,
  onSelect,
  onDelay,
}: {
  status: Status;
  groups: ProxyGroup[];
  group: ProxyGroup | null;
  busy: boolean;
  onSelectGroup: (name: string) => void;
  onMode: (mode: Mode) => void;
  onSelect: (group: string, member: string) => Promise<void>;
  onDelay: (group: string, names: string[]) => Promise<void>;
}) {
  const { t } = useI18n();
  const [query, setQuery] = useState("");
  const [testing, setTesting] = useState(false);
  const visible = visibleGroups(groups, status.mode);
  const disabled = busy || status.busy || !status.kernel;
  const manual = group ? isManual(group) : false;

  async function delay(target: string, names: string[]) {
    if (testing || disabled) return;
    setTesting(true);
    try {
      await onDelay(target, names);
    } finally {
      setTesting(false);
    }
  }

  const members = group
    ? group.members.filter((member) => {
        const value = query.trim().toLowerCase();
        if (!value) return true;
        return member.name.toLowerCase().includes(value) || member.type.toLowerCase().includes(value);
      })
    : [];

  return (
    <section className="page">
      <div className="page-head">
        <div>
          <h1>{t("代理")}</h1>
          <p className="muted small">
            {visible.length} {t("策略组")} · {uniqueNodeCount(groups)} {t("节点")}
          </p>
        </div>
        <div className="page-actions">
          <Segmented<Mode>
            value={status.mode}
            small
            label={t("路由模式")}
            disabled={disabled}
            onChange={onMode}
            options={[
              { value: "rule", label: t("规则") },
              { value: "global", label: t("全局") },
              { value: "direct", label: t("直连") },
            ]}
          />
        </div>
      </div>

      {!status.kernel ? (
        <div className="card">
          <Empty>{t("内核未运行")}</Empty>
        </div>
      ) : (
        <div className="proxy-layout">
          <aside className="group-list card" aria-label={t("策略组")}>
            {visible.length === 0 ? (
              <Empty>{t("暂无策略组")}</Empty>
            ) : (
              visible.map((item) => (
                <button
                  key={item.name}
                  type="button"
                  className={`group-item ${group?.name === item.name ? "active" : ""}`}
                  onClick={() => onSelectGroup(item.name)}
                >
                  <strong>{item.name}</strong>
                  <span>
                    {item.now || t("未选择节点")} · {typeLabel(item.type, t)}
                  </span>
                </button>
              ))
            )}
          </aside>

          <div className="node-panel">
            <div className="node-toolbar">
              <div className="group-title">
                {group ? `${group.name} · ${typeLabel(group.type, t)}` : ""}
              </div>
              <SearchBox
                value={query}
                onChange={setQuery}
                placeholder={t("搜索节点…")}
                className="grow"
              />
              <button
                type="button"
                className="btn"
                disabled={!group || testing || disabled}
                onClick={() => group && delay(group.name, [])}
              >
                {t("测试本组")}
              </button>
            </div>
            <div className="node-list card">
              {!group ? (
                <Empty>{t("暂无策略组")}</Empty>
              ) : members.length === 0 ? (
                <Empty>{t("没有匹配的节点")}</Empty>
              ) : (
                <>
                  {!manual ? <p className="notice">{t("此组由内核自动调度")}</p> : null}
                  {members.map((member) => (
                    <NodeRow
                      key={member.name}
                      group={group}
                      member={member}
                      manual={manual}
                      testing={testing}
                      disabled={disabled}
                      onSelect={() => {
                        if (!manual || group.now === member.name) return;
                        void onSelect(group.name, member.name);
                      }}
                      onTest={() => delay("", [member.name])}
                    />
                  ))}
                </>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

function NodeRow({
  group,
  member,
  manual,
  testing,
  disabled,
  onSelect,
  onTest,
}: {
  group: ProxyGroup;
  member: Member;
  manual: boolean;
  testing: boolean;
  disabled: boolean;
  onSelect: () => void;
  onTest: () => void;
}) {
  const { t } = useI18n();
  const selected = group.now === member.name;
  const isBuiltin = builtin.has(member.name);
  const delay = isBuiltin && member.name !== "DIRECT" ? null : member.delayKnown ? member.delay : null;
  const canTest = !isBuiltin && !testing && !disabled;
  return (
    <div
      className={`node-row ${selected ? "selected" : ""}`}
      role={manual ? "button" : undefined}
      tabIndex={manual ? 0 : undefined}
      aria-pressed={manual ? selected : undefined}
      onClick={manual ? onSelect : undefined}
      onKeyDown={
        manual
          ? (event) => {
              if (event.target !== event.currentTarget) return;
              if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
                onSelect();
              }
            }
          : undefined
      }
    >
      <span className="node-check">{selected ? <Check className="icon" /> : null}</span>
      <span className="node-name">
        <strong>{member.name}</strong>
        <span>
          {typeLabel(member.type, t)}
          {member.delayError ? ` · ${member.delayError}` : ""}
        </span>
      </span>
      <span className={`latency ${latencyClass(member.delayKnown ? member.delay : null)}`}>
        {delay == null ? "—" : latencyText(delay, t)}
      </span>
      <button
        type="button"
        className="icon-btn test-one"
        aria-label={`${t("测试本组")} ${member.name}`}
        title={`${t("测试本组")} ${member.name}`}
        disabled={!canTest}
        onClick={(event) => {
          event.stopPropagation();
          onTest();
        }}
      >
        <Play className="icon" />
      </button>
    </div>
  );
}
