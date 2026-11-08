package io.github.gopurr.nativeapp;

/** Runnable on the host JVM, without Android stubs or a device. */
public final class SessionMachineTest {
    public static void main(String[] args) throws Exception {
        cancelledPermissionCannotReconnect();
        replacingThePermissionPresenterKeepsIntent();
        oldWorkCannotStopNewConnection();
        repeatedConnectIsIdempotent();
        timeoutRejectsLateSuccess();
        staleCleanupCannotTakeNewTunnel();
        System.out.println("SessionMachine: 6 lifecycle regressions passed");
    }

    private static void cancelledPermissionCannotReconnect() {
        SessionMachine session = new SessionMachine();
        long pending = session.connect(true);
        require(session.claimPermission(pending), "Initial presenter must claim permission");
        long stopped = session.disconnect("permission_revoked");
        require(!session.permission(pending, true), "Late permission must not restart a disconnected session");
        require(session.stopped(stopped), "Disconnect must complete");
        require(session.snapshot().phase == SessionMachine.Phase.IDLE, "Revoked session must stay idle");
    }

    private static void replacingThePermissionPresenterKeepsIntent() {
        SessionMachine session = new SessionMachine();
        long pending = session.connect(true);
        require(session.claimPermission(pending), "First Activity claims the system dialog");
        require(!session.claimPermission(pending), "Two Activities cannot present the same request");
        session.releasePermission(pending);
        require(session.claimPermission(pending), "A replacement Activity may resume the same pending intent");
        require(session.permission(pending, true), "Permission remains attached to session intent");
    }

    private static void oldWorkCannotStopNewConnection() {
        SessionMachine session = new SessionMachine();
        long first = session.connect(false);
        long disconnect = session.disconnect("");
        long latest = session.connect(false);
        require(!session.connected(first), "An old connect cannot publish success");
        require(!session.stopped(disconnect), "An old disconnect cannot replace new connection state");
        require(session.connected(latest), "Newest explicit connection wins");
    }

    private static void repeatedConnectIsIdempotent() {
        SessionMachine session = new SessionMachine();
        long ticket = session.connect(false);
        require(session.connect(false) == ticket, "Repeated taps must not open duplicate TUNs");
        require(session.connected(ticket), "Connection must succeed");
        require(session.connect(false) == ticket, "Connected commands are idempotent");
    }

    private static void timeoutRejectsLateSuccess() {
        SessionMachine session = new SessionMachine();
        long ticket = session.connect(false);
        require(session.failed(ticket, "timeout"), "Timeout must settle the current task");
        require(!session.connected(ticket), "A timed-out operation cannot become connected later");
        require(session.snapshot().phase == SessionMachine.Phase.FAILED, "Timeout remains visible until the next action");
    }

    private static void staleCleanupCannotTakeNewTunnel() {
        TunnelOwnership<Object> tunnel = new TunnelOwnership<>();
        Object oldService = new Object();
        tunnel.assign(oldService, 1);
        require(tunnel.takeIf(oldService, 1) == oldService, "A task closes its own old tunnel");
        tunnel.assign(oldService, 3);
        require(tunnel.takeIf(oldService, 1) == null, "A queued timeout cannot stop a newer generation in the same service");
        require(tunnel.takeThrough(oldService, 2) == null, "An old service failure cannot stop a newer generation");
        Object newService = new Object();
        tunnel.assign(newService, 5);
        require(tunnel.takeIf(oldService, 0) == null, "An old onDestroy cannot take a replacement service's tunnel");
        require(tunnel.takeCurrent() == newService, "Only the current controller stop takes the latest tunnel");
    }

    private static void require(boolean value, String message) { if (!value) throw new AssertionError(message); }
}
