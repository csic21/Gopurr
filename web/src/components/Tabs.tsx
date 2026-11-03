import { Activity, Cable, Globe2, Terminal } from "lucide-react";
import { useI18n } from "../i18n";
import type { Page } from "../types";

const tabs: Array<{ page: Page; icon: typeof Activity; key: string }> = [
  { page: "overview", icon: Activity, key: "概览" },
  { page: "proxies", icon: Globe2, key: "代理" },
  { page: "connections", icon: Cable, key: "连接" },
  { page: "logs", icon: Terminal, key: "日志" },
];

export function Tabs({ page, onChange }: { page: Page; onChange: (page: Page) => void }) {
  const { t } = useI18n();
  return (
    <nav className="tabbar" aria-label="Sections">
      {tabs.map(({ page: value, icon: Icon, key }) => (
        <button
          key={value}
          type="button"
          className={`tab ${page === value ? "active" : ""}`}
          aria-label={t(key)}
          onClick={() => onChange(value)}
        >
          <Icon className="icon" aria-hidden="true" />
          <span>{t(key)}</span>
        </button>
      ))}
    </nav>
  );
}
