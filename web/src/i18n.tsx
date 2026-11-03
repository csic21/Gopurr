import { createContext, useCallback, useContext, useMemo, type ReactNode } from "react";

interface I18nValue {
  lang: string;
  t: (key: string) => string;
}

const I18nContext = createContext<I18nValue>({ lang: "en", t: (key) => key });

export function I18nProvider({
  lang,
  dictionary,
  children,
}: {
  lang: string;
  dictionary: Record<string, string>;
  children: ReactNode;
}) {
  const t = useCallback((key: string) => dictionary[key] || key, [dictionary]);
  const value = useMemo(() => ({ lang, t }), [lang, t]);
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  return useContext(I18nContext);
}
