package io.github.gopurr.nativeapp;

/** Maps semantic UI haptic intents to Android HapticFeedbackConstants by SDK level. */
final class Haptics {
    static final int NONE = -1;
    static final int LONG_PRESS = 0;
    static final int VIRTUAL_KEY = 1;
    static final int CLOCK_TICK = 4;
    static final int CONTEXT_CLICK = 6;
    static final int CONFIRM = 16;
    static final int REJECT = 17;
    static final int TOGGLE_ON = 21;
    static final int TOGGLE_OFF = 22;
    static final int SEGMENT_TICK = 26;

    private Haptics() { }

    static int constantFor(String kind, int sdkInt) {
        if (kind == null) return NONE;
        switch (kind) {
            case "selection": return sdkInt >= 34 ? SEGMENT_TICK : CLOCK_TICK;
            case "toggleOn": return sdkInt >= 34 ? TOGGLE_ON : VIRTUAL_KEY;
            case "toggleOff": return sdkInt >= 34 ? TOGGLE_OFF : CLOCK_TICK;
            case "impact": return CONTEXT_CLICK;
            case "confirm": return sdkInt >= 30 ? CONFIRM : CONTEXT_CLICK;
            case "reject": return sdkInt >= 30 ? REJECT : LONG_PRESS;
            default: return NONE;
        }
    }
}
