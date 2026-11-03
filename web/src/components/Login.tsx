import { useState, type FormEvent } from "react";
import { Moon, Sun } from "lucide-react";
import { ApiError, api } from "../api";
import { useI18n } from "../i18n";

export function Login({
  dark,
  expired,
  onToggleTheme,
  onLogin,
}: {
  dark: boolean;
  expired?: boolean;
  onToggleTheme: () => void;
  onLogin: (session: Awaited<ReturnType<typeof api.login>>) => void;
}) {
  const { t } = useI18n();
  const [token, setToken] = useState("");
  const [error, setError] = useState(expired ? t("会话已过期，请重新登录") : "");
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!token.trim() || busy) return;
    setBusy(true);
    setError("");
    try {
      const session = await api.login(token.trim());
      onLogin(session);
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) setError(t("令牌无效"));
        else if (err.status === 429) setError(t("登录尝试过多，请稍后再试"));
        else setError(err.message || t("未知错误"));
      } else {
        setError(t("网络请求失败"));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="login-shell">
      <div className="login-theme">
        <button
          type="button"
          className="icon-btn"
          aria-label={t("主题")}
          title={t("主题")}
          onClick={onToggleTheme}
        >
          {dark ? <Sun className="icon" /> : <Moon className="icon" />}
        </button>
      </div>
      <main className="login-card">
        <img className="login-logo" src="/assets/logo.svg" alt="Gopurr" />
        <h1>Gopurr</h1>
        <p className="muted">{t("远程控制台")}</p>
        <form className="login-form" autoComplete="off" onSubmit={submit}>
          <label className="field">
            <span>{t("访问令牌")}</span>
            <input
              type="password"
              value={token}
              autoFocus
              autoComplete="current-password"
              spellCheck={false}
              placeholder={t("输入访问令牌")}
              onChange={(event) => setToken(event.target.value)}
            />
          </label>
          <button className="btn primary wide" type="submit" disabled={busy || !token.trim()}>
            {busy ? t("正在连接…") : t("登录")}
          </button>
          <p className="form-error" role="alert">
            {error}
          </p>
        </form>
        <p className="login-note muted">
          {t("在 Gopurr 设置 → 网络 → 远程 Web 控制台中复制访问令牌。")}{" "}
          {t("令牌只保存在本机，请勿分享。")}
        </p>
      </main>
    </div>
  );
}
