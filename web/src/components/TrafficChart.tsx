import { useEffect, useRef } from "react";
import { useI18n } from "../i18n";
import type { TrafficSample } from "../types";

export function TrafficChart({ samples, dark }: { samples: TrafficSample[]; dark: boolean }) {
  const { t } = useI18n();
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const draw = () => drawChart(canvas, samples, t);
    draw();
    const observer = new ResizeObserver(draw);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [samples, dark, t]);

  return <canvas ref={canvasRef} id="traffic-chart" height={96} />;
}

function drawChart(canvas: HTMLCanvasElement, samples: TrafficSample[], t: (key: string) => string) {
  const width = canvas.clientWidth || 320;
  const height = canvas.clientHeight || 96;
  const dpr = window.devicePixelRatio || 1;
  if (canvas.width !== Math.round(width * dpr) || canvas.height !== Math.round(height * dpr)) {
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
  }
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, width, height);
  const styles = getComputedStyle(document.documentElement);
  const grid = styles.getPropertyValue("--border").trim() || "#ddd";
  const down = styles.getPropertyValue("--down").trim() || "#13745b";
  const up = styles.getPropertyValue("--up").trim() || "#315eac";
  const faint = styles.getPropertyValue("--faint").trim() || "#777";
  ctx.strokeStyle = grid;
  ctx.lineWidth = 1;
  for (let i = 1; i <= 3; i += 1) {
    const y = (height / 4) * i;
    ctx.globalAlpha = 0.5;
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.lineTo(width, y);
    ctx.stroke();
  }
  ctx.globalAlpha = 1;
  if (samples.length < 2) {
    ctx.fillStyle = faint;
    ctx.font = "11px system-ui, sans-serif";
    ctx.fillText(t("等待新数据"), 4, height / 2);
    return;
  }
  const max = Math.max(1, ...samples.map((sample) => Math.max(sample.down, sample.up)));
  const x = (index: number) => (index / (samples.length - 1)) * width;
  const y = (value: number) => height - 6 - (value / max) * (height - 14);
  const line = (key: "up" | "down", color: string, fill: boolean) => {
    if (fill) {
      ctx.beginPath();
      samples.forEach((sample, index) => {
        const px = x(index);
        const py = y(sample[key]);
        if (index === 0) ctx.moveTo(px, py);
        else ctx.lineTo(px, py);
      });
      ctx.lineTo(width, height);
      ctx.lineTo(0, height);
      ctx.closePath();
      const gradient = ctx.createLinearGradient(0, 0, 0, height);
      gradient.addColorStop(0, withAlpha(color, 0.2));
      gradient.addColorStop(1, withAlpha(color, 0));
      ctx.fillStyle = gradient;
      ctx.fill();
    }
    ctx.beginPath();
    samples.forEach((sample, index) => {
      const px = x(index);
      const py = y(sample[key]);
      if (index === 0) ctx.moveTo(px, py);
      else ctx.lineTo(px, py);
    });
    ctx.strokeStyle = color;
    ctx.lineWidth = 1.8;
    ctx.lineJoin = "round";
    ctx.stroke();
  };
  line("down", down, true);
  line("up", up, false);
  const last = samples[samples.length - 1];
  for (const [key, color] of [
    ["down", down],
    ["up", up],
  ] as const) {
    if (last[key] <= 0) continue;
    ctx.beginPath();
    ctx.arc(width - 2, y(last[key]), 2.6, 0, Math.PI * 2);
    ctx.fillStyle = color;
    ctx.fill();
  }
}

function withAlpha(color: string, alpha: number): string {
  if (color.startsWith("#") && (color.length === 7 || color.length === 4)) {
    const hex = color.length === 4
      ? color.slice(1).split("").map((char) => char + char).join("")
      : color.slice(1);
    const alphaHex = Math.round(Math.min(1, Math.max(0, alpha)) * 255).toString(16).padStart(2, "0");
    return `#${hex}${alphaHex}`;
  }
  return color;
}
