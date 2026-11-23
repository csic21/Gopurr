import { colorScheme, Image, Text, View, type AnimatedValue } from "nsh";
import { colors } from "./theme";
import { tr } from "./i18n";
import { Icon, StatusDot, Row, Label, tint } from "./ui";
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

export const navFlightGeometry = (titlebarHeight: number, layout: NavigationLayout, index: number) => {
  const rowTop = titlebarHeight + 62 + index * 38 + (index === 7 ? 12 : 0);
  const compactTop = (titlebarHeight - 30) / 2;
  const compactLeft = layout.left + layout.widths.slice(0, index).reduce((sum, width) => sum + width + 2, 0);
  const compactWidth = layout.widths[index];
  return {
    pill: { startX: 8, startY: rowTop, startW: 167, startH: 36, endX: compactLeft, endY: compactTop, endW: compactWidth, endH: 30 },
    item: { rowLeft: 21, startTextX: 48, startY: rowTop, endTextX: compactLeft + layout.padding + 0.5, endY: compactTop - 3, labelScaleTo: 12.5 / 13 },
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
  kernel?: boolean;
}) {
  const progress = p.progress;
  const dark = () => colorScheme() === "dark";
  const selectedBorder = () => dark() ? tint(colors().accent, "2e") : tint(colors().accent, "26");
  const slide = progress.interpolate({ inputRange: [0, 1], outputRange: [0, -184], extrapolate: "clamp" });
  const curtainOpacity = progress.interpolate({ inputRange: [0, 0.92, 1], outputRange: [1, 1, 0], extrapolate: "clamp" });
  const edgeShadowOpacity = progress.interpolate({ inputRange: [0, 0.12, 0.88, 1], outputRange: [0, 1, 1, 0], extrapolate: "clamp" });
  const footerOpacity = progress.interpolate({ inputRange: [0, 0.45], outputRange: [1, 0], extrapolate: "clamp" });
  const iconOpacity = progress.interpolate({ inputRange: [0, 0.42], outputRange: [1, 0], extrapolate: "clamp" });
  const iconShrink = progress.interpolate({ inputRange: [0, 0.42], outputRange: [1, 0.72], extrapolate: "clamp" });
  const iconNudge = progress.interpolate({ inputRange: [0, 0.42], outputRange: [0, 6], extrapolate: "clamp" });
  const labelScale = progress.interpolate({ inputRange: [0, 1], outputRange: [1, 12.5 / 13], extrapolate: "clamp" });
  const pillScaleY = progress.interpolate({ inputRange: [0, 1], outputRange: [1, 30 / 36], extrapolate: "clamp" });
  const brand = () => brandInterpolation(p.titlebarInset, p.titlebarHeight);
  const brandX = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().x });
  const brandY = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().y });
  const iconScale = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().iconScale });
  const brandScale = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().textScale });
  const brandTextX = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().textShiftX });
  const brandTextY = () => progress.interpolate({ inputRange: [0, 1], outputRange: brand().textShiftY });
  return <View accessibilityElementsHidden importantForAccessibility="no-hide-descendants" style={{ pointerEvents: "none", position: "absolute", left: 0, top: 0, width: "100%", height: "100%" }}>
    <View style={{ position: "absolute", left: 0, top: 0, width: 184, height: "100%", backgroundColor: colors().side, backgroundImage: "linear-gradient(180deg, " + colors().side + " 0%, " + colors().bg + " 100%)", borderRightWidth: 1, borderColor: colors().border, opacity: curtainOpacity, transform: [{ translateX: slide }] }}>
      <View style={{ position: "absolute", left: 14, right: 14, bottom: 14, paddingTop: 12, gap: 5, borderTopWidth: 1, borderColor: colors().border, backgroundColor: "transparent", opacity: footerOpacity }}>
        <Row gap={6}><StatusDot on={p.kernel ?? true} size={5.5} /><Label text={p.status} muted size={11} weight={500} /></Row>
        <Text style={{ fontSize: 10.5, color: colors().faint, numberOfLines: 1, fontVariant: "tabular-nums" }}>{`${p.proxies} ${tr("代理", "proxies")} · ${p.rules} ${tr("规则", "rules")}`}</Text>
      </View>
    </View>
    <View style={{ position: "absolute", left: 184, top: 0, width: 18, height: "100%", backgroundImage: "linear-gradient(90deg, " + (dark() ? "#00000038" : "#0f172a12") + " 0%, transparent 100%)", opacity: edgeShadowOpacity, transform: [{ translateX: slide }] }} />
    <View style={{ position: "absolute", left: 20, top: p.titlebarHeight + 15, flexDirection: "row", alignItems: "center", gap: 10, transform: [{ translateX: brandX().sub(20) }, { translateY: brandY().sub(p.titlebarHeight + 15) }] }}>
      <Image src={logo} style={{ width: 30, height: 30, objectFit: "contain", transform: [{ scale: iconScale() }], transformOrigin: "left top" }} />
      <Text style={{ fontSize: 15, fontWeight: "600", color: colors().text, numberOfLines: 1, transform: [{ translateX: brandTextX() }, { translateY: brandTextY() }, { scale: brandScale() }], transformOrigin: "left center" }}>Gopurr</Text>
    </View>
    <View style={{ position: "absolute", left: 0, top: 0, width: "100%", height: "100%" }}>
      {items.map((item, index) => {
        const geom = () => navFlightGeometry(p.titlebarHeight, p.layout, index).pill;
        const pillX = () => progress.interpolate({ inputRange: [0, 1], outputRange: [0, geom().endX - geom().startX] });
        const pillY = () => progress.interpolate({ inputRange: [0, 1], outputRange: [0, geom().endY - geom().startY] });
        const pillScaleX = () => progress.interpolate({ inputRange: [0, 1], outputRange: [1, geom().endW / geom().startW] });
        return <If when={p.page === item.id}><View style={{ position: "absolute", left: geom().startX, top: geom().startY, width: geom().startW, height: geom().startH, borderRadius: 8, backgroundColor: colors().selected, borderWidth: 1, borderColor: selectedBorder(), transformOrigin: "left top", transform: [{ translateX: pillX() }, { translateY: pillY() }, { scaleX: pillScaleX() }, { scaleY: pillScaleY }] }} /></If>;
      })}
      {items.map((item, index) => {
        const selected = () => p.page === item.id;
        const geom = () => navFlightGeometry(p.titlebarHeight, p.layout, index).item;
        const itemX = () => progress.interpolate({ inputRange: [0, 1], outputRange: [0, geom().endTextX - geom().startTextX] });
        const itemY = () => progress.interpolate({ inputRange: [0, 1], outputRange: [0, geom().endY - geom().startY] });
        return <View style={{ position: "absolute", left: geom().rowLeft, top: geom().startY, height: 36, flexDirection: "row", alignItems: "center", gap: 10, transform: [{ translateX: itemX() }, { translateY: itemY() }] }}>
          <View style={{ width: 17, height: 17, flexShrink: 0, alignItems: "center", justifyContent: "center", opacity: iconOpacity, transform: [{ translateX: iconNudge }, { scale: iconShrink }] }}>
            <Icon name={item.id} size={17} color={selected() ? colors().accent : colors().muted} />
          </View>
          <Text style={{ fontSize: 13, fontWeight: selected() ? "600" : "500", letterSpacing: -0.1, color: selected() ? colors().accent : colors().muted, numberOfLines: 1, transformOrigin: "left center", transform: [{ scale: labelScale }] }}>{tr(item.zh, item.en)}</Text>
        </View>;
      })}
    </View>
  </View>;
}
