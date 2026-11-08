package io.github.gopurr.nativeapp;

/** Small Android-independent authority for connection intent and task generations. */
public final class SessionMachine {
    public enum Phase { IDLE, AWAITING_PERMISSION, CONNECTING, CONNECTED, DISCONNECTING, FAILED }
    public static final class Snapshot {
        public final long generation;
        public final Phase phase;
        public final boolean wanted;
        public final String reason;
        Snapshot(long generation, Phase phase, boolean wanted, String reason) {
            this.generation = generation; this.phase = phase; this.wanted = wanted; this.reason = reason;
        }
    }
    private long generation;
    private long permissionClaim;
    private Phase phase = Phase.IDLE;
    private boolean wanted;
    private String reason = "";

    public synchronized Snapshot snapshot() { return new Snapshot(generation, phase, wanted, reason); }

    public synchronized long connect(boolean permissionNeeded) {
        if (wanted && (phase == Phase.AWAITING_PERMISSION || phase == Phase.CONNECTING || phase == Phase.CONNECTED)) return generation;
        generation++;
        wanted = true;
        phase = permissionNeeded ? Phase.AWAITING_PERMISSION : Phase.CONNECTING;
        reason = "";
        return generation;
    }

    public synchronized boolean claimPermission(long ticket) {
        if (ticket != generation || !wanted || phase != Phase.AWAITING_PERMISSION || permissionClaim == ticket) return false;
        permissionClaim = ticket;
        return true;
    }

    public synchronized void releasePermission(long ticket) {
        if (generation == ticket && phase == Phase.AWAITING_PERMISSION && permissionClaim == ticket) permissionClaim = 0;
    }

    public synchronized boolean permission(long ticket, boolean granted) {
        if (ticket != generation || !wanted || phase != Phase.AWAITING_PERMISSION) return false;
        if (granted) phase = Phase.CONNECTING;
        else { wanted = false; phase = Phase.FAILED; reason = "permission_denied"; }
        return true;
    }

    public synchronized long disconnect(String why) {
        generation++;
        wanted = false;
        phase = Phase.DISCONNECTING;
        reason = why == null ? "" : why;
        return generation;
    }

    public synchronized boolean currentConnect(long ticket) {
        return generation == ticket && wanted && (phase == Phase.CONNECTING || phase == Phase.CONNECTED);
    }

    public synchronized boolean connected(long ticket) {
        if (!currentConnect(ticket)) return false;
        phase = Phase.CONNECTED; reason = ""; return true;
    }

    public synchronized boolean stopped(long ticket) {
        if (generation != ticket || wanted) return false;
        phase = Phase.IDLE; return true;
    }

    public synchronized boolean failed(long ticket, String why) {
        if (generation != ticket) return false;
        wanted = false; phase = Phase.FAILED; reason = why == null ? "vpn_failed" : why; return true;
    }
}
