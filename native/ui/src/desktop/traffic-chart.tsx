import { Canvas, cleanup, colorScheme, createAnimatedValue, effect, lifecycle, reduceMotion, signal, Text, View } from 'nsh';
import type { MouseEvent } from 'nsh';
import { colors } from './theme';
import { tr } from './i18n';
import { elevation, fmtRate } from './ui';
import { trafficAt, trafficCurve, trafficRest, trafficTransition } from './traffic';
import type { TrafficFrame, TrafficSample } from './traffic';

/**
 * The live traffic surface. It draws its own grid, two data-driven gradient
 * fills and the newest sample; hovering turns the axis into an inspector
 * without moving the layout.
 */
export function TrafficChart(p: { history: () => TrafficSample[]; compact?: boolean }) {
  let previous: TrafficSample[] = [], visible: TrafficFrame = trafficRest([]), signature = '', frame = 0, latestSample: TrafficSample | undefined;
  const display = signal(visible), hover = signal(-1), chartWidth = signal(0);
  const show = (value: TrafficFrame) => { visible = value; display.set(value); };
  effect(() => {
    const next = p.history().map(v => ({ up: Math.max(0, Number.isFinite(v.up) ? v.up : 0), down: Math.max(0, Number.isFinite(v.down) ? v.down : 0) }));
    const key = JSON.stringify(next), paused = reduceMotion() || lifecycle() !== 'active';
    latestSample = next[next.length - 1];
    if (paused) { cancelAnimationFrame(frame); previous = next; signature = key; show(trafficRest(next)); return; }
    if (key === signature) return;
    signature = key; cancelAnimationFrame(frame);
    const transition = trafficTransition(previous, next, visible); previous = next;
    if (!transition) { show(trafficRest(next)); return; }
    const start = Date.now();
    const tick = () => { const t = Math.min(1, (Date.now() - start) / 400); show(trafficAt(transition, t)); if (t < 1) frame = requestAnimationFrame(tick); };
    frame = requestAnimationFrame(tick);
  });
  cleanup(() => cancelAnimationFrame(frame));
  const track = (e: MouseEvent) => {
    const width = chartWidth() - 8;
    if (!(width > 0)) return;
    const fx = (e.position.x - 4) / width;
    let best = -1, bestDistance = Infinity;
    for (const point of display().points) { const distance = Math.abs(point.x - fx); if (distance < bestDistance) { bestDistance = distance; best = point.index; } }
    if (best !== hover()) hover.set(best);
  };
  const history = () => p.history();
  const hoveredIndex = () => { const value = hover(); return value >= 0 && value < history().length ? value : -1; };
  const hovered = () => (hoveredIndex() >= 0 ? history()[hoveredIndex()] : undefined);
  const secondsAgo = () => Math.max(0, history().length - 1 - hover());
  const tipLeft = () => {
    const width = Math.max(1, chartWidth() - 8), count = Math.max(1, history().length - 1);
    const x = 4 + (hoveredIndex() / count) * width;
    return Math.max(0, Math.min(Math.max(0, chartWidth() - 208), x - 96));
  };
  return <View style={{ flex: 1, minHeight: 0, gap: 5 }}>
    <View style={{ flex: 1, minHeight: 0 }} onMouseMove={track} onMouseLeave={() => hover.set(-1)} onLayout={({ bounds }) => { if (Math.abs(bounds.size.width - chartWidth()) > 0.5) chartWidth.set(bounds.size.width); }}>
      <Canvas testID="traffic-chart" style={{ flex: 1, cursor: "crosshair" }} draw={(cx, size) => {
        const current = display(), palette = colors(), bottom = size.height - 4, height = Math.max(1, size.height - 13), width = Math.max(1, size.width - 8), dark = colorScheme() === 'dark';
        // Three quiet rules give the eye a scale without becoming a frame.
        cx.lineWidth = 1; cx.strokeStyle = palette.grid + "e6";
        for (let line = 1; line < 4; line++) { const y = bottom - height * line / 4; cx.beginPath(); cx.moveTo(0, y); cx.lineTo(size.width, y); cx.stroke(); }
        cx.strokeStyle = palette.border; cx.beginPath(); cx.moveTo(0, bottom + 0.5); cx.lineTo(size.width, bottom + 0.5); cx.stroke();
        const channels = (['down', 'up'] as const).map(key => ({ key, color: palette[key], curve: trafficCurve(current.points.map(v => ({ x: 4 + v.x * width, y: bottom - v[key] / current.scale * height }))) }));
        const path = (curve: NonNullable<ReturnType<typeof trafficCurve>>) => { cx.beginPath(); cx.moveTo(curve.start.x, curve.start.y); for (const s of curve.segments) cx.bezierCurveTo(s.c1.x, s.c1.y, s.c2.x, s.c2.y, s.end.x, s.end.y); };
        // Paint the two quiet fills first so neither masks the other channel's line.
        for (const { curve, color } of channels) { if (!curve) continue; path(curve); const end = curve.segments[curve.segments.length - 1]?.end ?? curve.start; cx.lineTo(end.x, bottom); cx.lineTo(curve.start.x, bottom); cx.closePath(); const fill = cx.createLinearGradient(0, 2, 0, bottom); fill.addColorStop(0, color + (dark ? "38" : "30")); fill.addColorStop(1, color + "00"); cx.fillStyle = fill; cx.fill(); }
        cx.lineCap = 'round'; cx.lineJoin = 'round';
        for (const { curve, color, key } of channels) {
          if (!curve) continue;
          path(curve); cx.strokeStyle = color; cx.lineWidth = 2; cx.shadowColor = color + "33"; cx.shadowBlur = 3; cx.stroke(); cx.shadowBlur = 0;
          const latest = current.points[current.points.length - 1];
          if (latest?.[key] > 0 && (latestSample?.[key] ?? 0) > 0) {
            const x = 4 + latest.x * width, y = bottom - latest[key] / current.scale * height;
            cx.beginPath(); cx.arc(x, y, 3.2, 0, Math.PI * 2); cx.fillStyle = color + "2e"; cx.fill();
            cx.beginPath(); cx.arc(x, y, 2.2, 0, Math.PI * 2); cx.fillStyle = color; cx.fill(); cx.strokeStyle = palette.surface; cx.lineWidth = 1.5; cx.stroke();
          }
        }
        const highlighted = current.points.find(v => v.index === hover());
        if (highlighted) {
          const x = 4 + highlighted.x * width;
          cx.setLineDash([3, 3]); cx.strokeStyle = palette.faint + "8c"; cx.lineWidth = 1; cx.beginPath(); cx.moveTo(x, 2); cx.lineTo(x, bottom); cx.stroke(); cx.setLineDash([]);
          for (const { key, color } of channels) { const y = bottom - highlighted[key] / current.scale * height; cx.beginPath(); cx.arc(x, y, 3.4, 0, Math.PI * 2); cx.fillStyle = color; cx.fill(); cx.strokeStyle = palette.surface; cx.lineWidth = 1.6; cx.stroke(); }
        }
      }} />
      <If when={hoveredIndex() >= 0}>
        <View style={{ position: "absolute", top: 4, left: tipLeft(), flexDirection: "row", alignItems: "center", gap: 10, paddingHorizontal: 10, paddingVertical: 6, borderRadius: 8, backgroundColor: colors().surface, borderWidth: 1, borderColor: colors().border, boxShadow: elevation("popover"), pointerEvents: "none" }}>
          <Text style={{ fontSize: 10.5, color: colors().faint, fontVariant: "tabular-nums", numberOfLines: 1 }}>{tr(`${secondsAgo()} 秒前`, `${secondsAgo()} s ago`)}</Text>
          <Text style={{ fontSize: 10.5, fontWeight: "600", color: colors().down, fontVariant: "tabular-nums", numberOfLines: 1 }}>{`↓ ${fmtRate(hovered()?.down ?? 0)}`}</Text>
          <Text style={{ fontSize: 10.5, fontWeight: "600", color: colors().up, fontVariant: "tabular-nums", numberOfLines: 1 }}>{`↑ ${fmtRate(hovered()?.up ?? 0)}`}</Text>
        </View>
      </If>
    </View>
    <View style={{ flexDirection: "row", alignItems: "center", justifyContent: "space-between", gap: 10, height: 14 }}>
      <Text style={{ fontSize: 10.5, color: colors().faint, fontVariant: "tabular-nums", numberOfLines: 1 }}>{history().length > 1 ? tr(`${Math.max(0, history().length - 1)} 秒前`, `${Math.max(0, history().length - 1)} s ago`) : "—"}</Text>
      <Text style={{ fontSize: 10.5, color: colors().faint, numberOfLines: 1 }}>{tr("现在", "Now")}</Text>
    </View>
  </View>;
}

/**
 * A rate readout: the figure takes the channel color and the unit stays quiet,
 * so the eye reads magnitude before the label.
 */
export function TrafficRate(p: { value: string; color: string; size?: number }) {
  const opacity = createAnimatedValue(1); let previous = p.value;
  effect(() => { const value = p.value, paused = reduceMotion() || lifecycle() !== 'active'; if (paused) { previous = value; opacity.set(1); return; } if (value === previous) return; previous = value; opacity.set(.55); void opacity.timing(1, { duration: 180, easing: 'easeOut' }); });
  cleanup(() => { void opacity.stop(); });
  // NSH reacts inside JSX expressions: derive the figure and unit there so a
  // new sample updates both halves of the readout.
  return <View style={{ flexDirection: "row", alignItems: "baseline", gap: 4, opacity }}>
    <Text style={{ fontSize: p.size ?? 30, lineHeight: (p.size ?? 30) + 6, fontWeight: '600', letterSpacing: -0.9, color: p.color, fontVariant: 'tabular-nums' }}>{p.value.includes(' ') ? p.value.slice(0, p.value.lastIndexOf(' ')) : p.value}</Text>
    <If when={p.value.includes(' ')}><Text style={{ fontSize: (p.size ?? 30) * 0.44, fontWeight: '500', letterSpacing: -0.2, color: p.color, opacity: .72, fontVariant: 'tabular-nums' }}>{p.value.slice(p.value.lastIndexOf(' ') + 1)}</Text></If>
  </View>;
}
