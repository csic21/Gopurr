import { useEffect, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Search, X } from "lucide-react";
import type { ToastMessage } from "../types";
import { useI18n } from "../i18n";

export function IconButton({
  label,
  onClick,
  disabled,
  children,
  className = "",
}: {
  label: string;
  onClick?: () => void;
  disabled?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <button
      type="button"
      className={`icon-btn ${className}`}
      aria-label={label}
      title={label}
      onClick={onClick}
      disabled={disabled}
    >
      {children}
    </button>
  );
}

export function Switch({
  checked,
  disabled,
  label,
  onChange,
}: {
  checked: boolean;
  disabled?: boolean;
  label: string;
  onChange: (value: boolean) => void;
}) {
  return (
    <label className="switch">
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        aria-label={label}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span />
    </label>
  );
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  small,
  disabled,
  label,
}: {
  value: T;
  options: Array<{ value: T; label: string }>;
  onChange: (value: T) => void;
  small?: boolean;
  disabled?: boolean;
  label: string;
}) {
  return (
    <div className={`segmented ${small ? "small" : ""}`} role="group" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          className={value === option.value ? "active" : ""}
          aria-pressed={value === option.value}
          disabled={disabled}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  );
}

export function SearchBox({
  value,
  onChange,
  placeholder,
  className = "",
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  className?: string;
}) {
  return (
    <label className={`search ${className}`}>
      <Search className="icon" aria-hidden="true" />
      <input
        type="search"
        value={value}
        aria-label={placeholder}
        placeholder={placeholder}
        autoComplete="off"
        spellCheck={false}
        onChange={(event) => onChange(event.target.value)}
      />
    </label>
  );
}

export function Pill({
  tone = "neutral",
  children,
}: {
  tone?: "neutral" | "ok" | "bad" | "warn";
  children: ReactNode;
}) {
  return <span className={`pill ${tone}`}>{children}</span>;
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function Modal({
  title,
  onClose,
  children,
  footer,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  footer?: ReactNode;
}) {
  const { t } = useI18n();
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = "";
    };
  }, [onClose]);

  return createPortal(
    <div
      className="modal-backdrop"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
    >
      <div className="modal" role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-head">
          <h2>{title}</h2>
          <button type="button" className="icon-btn" aria-label={t("关闭")} onClick={onClose}>
            <X className="icon" />
          </button>
        </div>
        {children}
        {footer ? <div className="detail-actions">{footer}</div> : null}
      </div>
    </div>,
    document.body,
  );
}

export function Toast({ toast, onDismiss }: { toast: ToastMessage | null; onDismiss: () => void }) {
  useEffect(() => {
    if (!toast) return;
    const timer = window.setTimeout(onDismiss, 2600);
    return () => window.clearTimeout(timer);
  }, [toast, onDismiss]);
  if (!toast) return null;
  return (
    <div className={`toast ${toast.bad ? "bad" : ""}`} role="status">
      {toast.text}
    </div>
  );
}

export function Loading({ label }: { label: string }) {
  return (
    <div className="loading">
      <span className="spinner" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}
