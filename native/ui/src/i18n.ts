import { locale, signal } from "nsh";

export const language = signal<"system" | "zh-Hans" | "en">("system");
export function tr(zh: string, en: string): string {
  return language() === "zh-Hans" || (language() === "system" && locale().toLowerCase().startsWith("zh")) ? zh : en;
}
