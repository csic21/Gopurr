package io.github.gopurr.nativeapp;

import java.util.Locale;

/** Shared formatting for the compact and expanded native notification. */
public final class TrafficText {
    private TrafficText() { }

    public static String bytes(long value) {
        double size = Math.max(0, value);
        String[] units = {"B", "KB", "MB", "GB", "TB", "PB", "EB"};
        int unit = 0;
        while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++; }
        return unit == 0 ? String.format(Locale.ROOT, "%.0f B", size)
                : String.format(Locale.ROOT, size < 10 ? "%.1f %s" : "%.0f %s", size, units[unit]);
    }

    public static String rates(boolean chinese, long down, long up) {
        return (chinese ? "下载 " : "Down ") + bytes(down) + "/s   "
                + (chinese ? "上传 " : "Up ") + bytes(up) + "/s";
    }

    public static String totals(boolean chinese, long down, long up) {
        return (chinese ? "累计下载 " : "Downloaded ") + bytes(down) + "   "
                + (chinese ? "上传 " : "Uploaded ") + bytes(up);
    }
}
