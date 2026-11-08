import { Image, Text, View, windowSize } from "nsh";
import type { ImageAsset, JSX } from "nsh";
import { Button, Input } from "nsh/base";
import { colors } from "../theme";
import { mutationBusy } from "./store";

export const compact = () => windowSize().width < 680;
export function bytes(value: number, rate = false): string {
  let size = Math.max(0, value || 0), unit = 0;
  const units = ["B", "KB", "MB", "GB", "TB"];
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
  return `${size.toFixed(unit ? 1 : 0)} ${units[unit]}${rate ? "/s" : ""}`;
}
export function Icon(props: { source: ImageAsset; selected?: boolean; size?: number }) {
  return <Image src={props.source} style={{ width: props.size ?? 18, height: props.size ?? 18, objectFit: "contain", tintColor: props.selected ? colors().accent : colors().muted }} />;
}
export function Label(props: { text: string; muted?: boolean; size?: number; bold?: boolean }) {
  return <Text style={{ color: props.muted ? colors().muted : colors().text, fontSize: props.size ?? 13, fontWeight: props.bold ? "600" : "400", lineHeight: (props.size ?? 13) * 1.45 }}>{props.text}</Text>;
}
export function Action(props: { label: string; onPress: () => void; primary?: boolean; danger?: boolean; disabled?: boolean; testID?: string }) {
  return <Button testID={props.testID} onPress={props.onPress} disabled={props.disabled ?? mutationBusy()}
    style={{ minHeight: 44, paddingHorizontal: 12, paddingVertical: 8, borderRadius: 8, alignItems: "center", justifyContent: "center", backgroundColor: props.primary ? colors().accent : colors().surface, borderWidth: props.primary ? 0 : 1, borderColor: colors().border }}
    hoverStyle={{ backgroundColor: props.primary ? colors().accentHover : colors().hover }} focusStyle={{ outlineWidth: 2, outlineColor: colors().accent, outlineOffset: 2 }} activeStyle={{ opacity: 0.75 }} disabledStyle={{ opacity: 0.45 }}>
    <Text style={{ color: props.primary ? colors().accentText : props.danger ? colors().danger : colors().text, fontSize: 13, fontWeight: "500" }}>{props.label}</Text>
  </Button>;
}
export function Panel(props: { children?: JSX.Element }) {
  return <View style={{ padding: 16, borderRadius: 12, backgroundColor: colors().surface, gap: 12 }}>{props.children}</View>;
}
export function Heading(props: { title: string; description: string; action?: () => JSX.Element }) {
  return <View style={{ gap: 8, paddingBottom: 4 }}><View style={{ flexDirection: "row", gap: 12, alignItems: "center", justifyContent: "space-between" }}>
    <Text role="header" style={{ fontSize: 22, fontWeight: "600", color: colors().text }}>{props.title}</Text>{props.action?.()}
  </View><Label text={props.description} muted /></View>;
}
export function Empty(props: { title: string; description: string; action?: () => JSX.Element }) {
  return <View style={{ alignItems: "center", justifyContent: "center", paddingVertical: 32, paddingHorizontal: 16, gap: 12 }}>
    <Text style={{ fontSize: 16, fontWeight: "600", color: colors().text, textAlign: "center" }}>{props.title}</Text>
    <Text style={{ fontSize: 13, color: colors().muted, textAlign: "center", maxWidth: 380 }}>{props.description}</Text>{props.action?.()}
  </View>;
}
export function Field(props: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; multiline?: boolean; disabled?: boolean; testID?: string }) {
  return <View style={{ gap: 6 }}><Label text={props.label} bold size={12} />
    <Input testID={props.testID} value={props.value} onChange={({ value }) => props.onChange(value)} placeholder={props.placeholder} multiline={props.multiline} autoCapitalize="none" autoCorrect={false} autoComplete="off" readonly={props.disabled}
      style={{ minHeight: 44, height: props.multiline ? 160 : undefined, padding: 12, borderWidth: 1, borderColor: colors().border, borderRadius: 8, color: colors().text, fontSize: props.multiline ? 12 : 14, fontFamily: props.multiline ? "monospace" : undefined, backgroundColor: colors().bg, placeholderColor: colors().faint }} />
  </View>;
}
