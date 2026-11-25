package io.github.gopurr.nativeapp;

/** Runnable on the host JVM, without Android stubs or a device. */
public final class HapticsTest {
    public static void main(String[] args) {
        mapsAllKindsAcrossSdkLevels();
        rejectsUnknownOrNullKinds();
        System.out.println("Haptics: 2 regressions passed");
    }

    private static void mapsAllKindsAcrossSdkLevels() {
        for (int sdk : new int[]{28, 30, 33, 34, 36}) {
            require(Haptics.constantFor("selection", sdk) == (sdk >= 34 ? Haptics.SEGMENT_TICK : Haptics.CLOCK_TICK),
                    "Unexpected selection haptic at SDK " + sdk);
            require(Haptics.constantFor("toggleOn", sdk) == (sdk >= 34 ? Haptics.TOGGLE_ON : Haptics.VIRTUAL_KEY),
                    "Unexpected toggleOn haptic at SDK " + sdk);
            require(Haptics.constantFor("toggleOff", sdk) == (sdk >= 34 ? Haptics.TOGGLE_OFF : Haptics.CLOCK_TICK),
                    "Unexpected toggleOff haptic at SDK " + sdk);
            require(Haptics.constantFor("impact", sdk) == Haptics.CONTEXT_CLICK,
                    "Unexpected impact haptic at SDK " + sdk);
            require(Haptics.constantFor("confirm", sdk) == (sdk >= 30 ? Haptics.CONFIRM : Haptics.CONTEXT_CLICK),
                    "Unexpected confirm haptic at SDK " + sdk);
            require(Haptics.constantFor("reject", sdk) == (sdk >= 30 ? Haptics.REJECT : Haptics.LONG_PRESS),
                    "Unexpected reject haptic at SDK " + sdk);
        }
    }

    private static void rejectsUnknownOrNullKinds() {
        require(Haptics.constantFor(null, 36) == Haptics.NONE, "Null kind must return NONE");
        require(Haptics.constantFor("", 36) == Haptics.NONE, "Empty kind must return NONE");
        require(Haptics.constantFor("unknown", 36) == Haptics.NONE, "Unknown kind must return NONE");
    }

    private static void require(boolean value, String message) {
        if (!value) throw new AssertionError(message);
    }
}
