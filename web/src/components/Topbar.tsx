import { LogOut, Moon, RefreshCw, Sun } from "lucide-react";
import { useI18n } from "../i18n";
import type { Status } from "../types";
import { IconButton, Pill } from "./ui";

export function Topbar({
  status,
  dark,
  onToggleTheme,
  onRefresh,
  onLogout,
  busy,
}: {
  status: Status | null;
  dark: boolean;
  onToggleTheme: () => void;
  onRefresh: () => void;
  onLogout: () => void;
  busy: boolean;
}) {
  const { t } = useI18n();
  const kernel = status?.kernel === true;
  return (
    <header className="topbar">
      <div className="brand">
        <img src="/assets/logo.svg" alt="" width={30} height={30} />
        <div className="brand-text">
          <strong>Gopurr</strong>
          <span className="muted">{t("远程控制台")}</span>
        </div>
      </div>
      <div className="topbar-actions">
        <Pill tone={status ? (kernel ? "ok" : "bad") : "warn"}>
          <span className="dot" />
          <span>{status ? (kernel ? t("内核运行中") : t("内核未运行")) : t("连接中")}</span>
        </Pill>
        <IconButton label={t("主题")} onClick={onToggleTheme}>
          {dark ? <Sun className="icon" /> : <Moon className="icon" />}
        </IconButton>
        <IconButton label={t("刷新")} onClick={onRefresh} disabled={busy}>
          <RefreshCw className={`icon ${busy ? "spin" : ""}`} />
        </IconButton>
        <IconButton label={t("退出登录")} onClick={onLogout}>
          <LogOut className="icon" />
        </IconButton>
      </div>
    </header>
  );
}
