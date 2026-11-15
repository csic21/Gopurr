import { Image, Text, View, type AnimatedValue } from "nsh";
import { colors } from "./theme";
import { tr } from "./i18n";
import { elevation, Icon, StatusDot, Row, Label, tint } from "./ui";
import type { NavigationLayout } from "./navigation";
import type { Page } from "./types";
import logo from "../../assets/sidebar.png";

const items: { id: Page; zh: string; en: string }[] = [
  { id: "overview", zh: "概览", en: "Overview" },
  { id: "subs", zh: "订阅", en: "Profiles" },
  { id: "proxies", zh: "代理", en: "Proxies" },
  { id: "conns", zh: "连接", en: "Connections" },
  { id: "mesh", zh: "组网", en: "Mesh" },
  { id: "rules", zh: "规则", en: "Rules" },
  { id: "logs", zh: "日志", en: "Logs" },
  { id: "settings", zh: "设置", en: "Settings" },
];

// The visible half of the sidebar transition. The app's real navigation
// commits once; this copy draws the old layout sliding and the new top bar
// taking its place. It takes no input and duplicates no screen-reader targets.
export const BRAND_TRAVEL = { fromX: 20, fromGap: 10, fromIcon: 30, fromText: 15, toInset: 8, toGap: 6, toIcon: 24, toText: 13 } as const;
export const brandLayout = (titlebarInset: number, titlebarHeight: number, collapsed: boolean) => collapsed
  ? { left: titlebarInset + BRAND_TRAVEL.toInset, top: (titlebarHeight - BRAND_TRAVEL.toIcon) / 2, gap: BRAND_TRAVEL.toGap, icon: BRAND_TRAVEL.toIcon, text: BRAND_TRAVEL.toText }
  : { left: BRAND_TRAVEL.fromX, top: titlebarHeight + 15, gap: BRAND_TRAVEL.fromGap, icon: BRAND_TRAVEL.fromIcon, text: BRAND_TRAVEL.fromText };
export const brandInterpolation = (titlebarInset: number, titlebarHeight: number) => {
  const from = brandLayout(titlebarInset, titlebarHeight, false);
  const to = brandLayout(titlebarInset, titlebarHeight, true);
  return {
    x: [from.left, to.left],
    y: [from.top, to.top],
    iconScale: [1, to.icon / from.icon],
    textScale: [1, to.text / from.text],
    textShiftX: [0, (to.left + to.icon + to.gap) - (to.left + from.icon + from.gap)],
    textShiftY: [0, (to.top + to.icon / 2) - (to.top + from.icon / 2)],
  };
};

export function NavigationFlight(p: {
  progress: AnimatedValue;
  page: Page;
  titlebarInset: number;
  titlebarHeight: number;
  layout: NavigationLayout;
  status: string;
  proxies: number;
  rules: number;
}) {
  const { progress, titlebarHeight, titlebarInset, layout } = p;
  const top = (index: number) => titlebarHeight + 62 + index * 38 + (index === 7 ? 12 : 0);
  const compactTop = (titlebarHeight - 30) / 2;
  const compactLeft = (index: number) => layout.left + layout.widths.slice(0, index).reduce((sum, width) => sum + width + 2, 0);
  const compactWidth = (index: number) => layout.widths[index];
  const slide = progress.interpolate({ inputRange: [0, 1], outputRange: [0, -184] });
  const fade = progress.interpolate({ inputRange: [0, 1], outputRange: [1, 0] });
  const brand = brandInterpolation(titlebarInset, titlebarHeight);
  const brandX = progress.interpolate({ inputRange: [0, 1], outputRange: brand.x });
  const brandY = progress.interpolate({ inputRange: [0, 1], outputRange: brand.y });
  const iconScale = progress.interpolate({ inputRange: [0, 1], outputRange: brand.iconScale });
  const brandScale = progress.interpolate({ inputRange: [0, 1], outputRange: brand.textScale });
  const brandTextX = progress.interpolate({ inputRange: [0, 1], outputRange: brand.textShiftX });
  const brandTextY = progress.interpolate({ inputRange: [0, 1], outputRange: brand.textShiftY });
  return <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ pointerEvents: "none", position: "absolute", left: 0, top: 0, width: "100%", height: "100%" }}>
    <View style={{ position: "absolute", left: 0, top: 0, width: 184, height: "100%", backgroundColor: colors().side, backgroundImage: "linear-gradient(180deg, " + colors().side + " 0%, " + colors().bg + " 100%)", borderRightWidth: 1, borderColor: colors().border, opacity: fade, transform: [{ translateX: slide }] }}>
      {items.map((item, index) => {
        const selected = p.page === item.id;
        return <View style={{ position: "absolute", left: 8, top: top(index), width: 167, height: 36, flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 13, borderRadius: 9, backgroundColor: selected ? colors().selected : "transparent", borderLeftWidth: selected ? 2.5 : 0, borderLeftColor: colors().accent }}>
          <Icon name={item.id} size={17} color={selected ? colors().accent : colors().muted} />
          <Text style={{ fontSize: 13, fontWeight: selected ? "600" : "500", color: selected ? colors().accent : colors().muted, numberOfLines: 1 }}>{tr(item.zh, item.en)}</Text>
        </View>;
      })}
      <View style={{ position: "absolute", left: 14, bottom: 14, width: 156, padding: 12, gap: 7, borderRadius: 13, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("surface") }}>
        <Row gap={7}><StatusDot on={true} size={6} glow /><Label text={p.status} size={11.5} weight={600} /></Row>
        <Text style={{ fontSize: 10.5, color: colors().faint, numberOfLines: 1, fontVariant: "tabular-nums" }}>{`${p.proxies} ${tr("代理", "proxies")} · ${p.rules} ${tr("规则", "rules")}`}</Text>
      </View>
    </View>
    <View style={{ position: "absolute", left: 20, top: titlebarHeight + 15, flexDirection: "row", alignItems: "center", gap: 10, transform: [{ translateX: brandX.sub(20) }, { translateY: brandY.sub(titlebarHeight + 15) }] }}>
      <Image src={logo} style={{ width: 30, height: 30, objectFit: "contain", transform: [{ scale: iconScale }], transformOrigin: "left top" }} />
      <Text style={{ fontSize: 15, fontWeight: "600", color: colors().text, numberOfLines: 1, transform: [{ translateX: brandTextX }, { translateY: brandTextY }, { scale: brandScale }], transformOrigin: "left center" }}>Gopurr</Text>
    </View>
    <View style={{ position: "absolute", left: 0, top: 0, width: "100%", height: "100%" }}>
      {items.map((item, index) => {
        const selected = p.page === item.id;
        const startX = 8, endX = compactLeft(index), startY = top(index), endY = compactTop;
        const startW = 167, endW = compactWidth(index);
        return <View style={{ position: "absolute", left: endX, top: endY, width: endW, height: 30, alignItems: "center", justifyContent: "center", paddingHorizontal: layout.padding, borderRadius: 9, backgroundColor: selected ? colors().selected : "transparent", boxShadow: selected ? [{ x: 0, y: 1, blur: 2, color: tint(colors().accent, "14") }] : undefined, opacity: progress, transformOrigin: "left center", transform: [{ translateX: progress.interpolate({ inputRange: [0, 1], outputRange: [startX - endX, 0] }) }, { translateY: progress.interpolate({ inputRange: [0, 1], outputRange: [startY - endY, 0] }) }, { scaleX: progress.interpolate({ inputRange: [0, 1], outputRange: [startW / endW, 1] }) }] }}>
          <Text style={{ fontSize: 12.5, fontWeight: selected ? "600" : "500", color: selected ? colors().accent : colors().muted, numberOfLines: 1 }}>{tr(item.zh, item.en)}</Text>
        </View>;
      })}
    </View>
  </View>;
}
