package io.github.gopurr.nativeapp;

import android.content.Context;
import android.content.Intent;
import android.content.SharedPreferences;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.net.VpnService;
import android.os.Handler;
import android.os.Looper;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.function.Consumer;

/** Process-owned connection intent, independent of Activity and long profile downloads. */
public final class SessionController {
    public interface Listener { void changed(JSONObject snapshot); }
    private static SessionController instance;
    private static final Handler MAIN = new Handler(Looper.getMainLooper());
    private final ExecutorService control = Executors.newSingleThreadExecutor();
    private final SessionMachine machine = new SessionMachine();
    private final TunnelOwnership<GopurrVpnService> tunnel = new TunnelOwnership<>();
    private final Context context;
    private final SharedPreferences preferences;
    private final CopyOnWriteArrayList<Listener> listeners = new CopyOnWriteArrayList<>();
    private final Map<Long, List<Consumer<String>>> waiters = new HashMap<>();
    private volatile GopurrVpnService service;
    private AppScope scope;
    private boolean scopeValid = true;
    private String errorMessage = "";

    public static synchronized SessionController get(Context context) {
        if (instance == null) instance = new SessionController(context.getApplicationContext());
        return instance;
    }

    private SessionController(Context context) {
        this.context = context;
        preferences = context.getSharedPreferences("android-session", Context.MODE_PRIVATE);
        try { scope = AppScope.parse(new JSONObject(preferences.getString("scope", "{}")), context.getPackageName()); }
        catch (Exception ignored) {
            scope = AppScope.all(); scopeValid = false;
            machine.failed(machine.connect(false), "invalid_scope");
            errorMessage = "Saved application scope could not be read. Choose and save it again before connecting.";
        }
    }

    public void addListener(Listener listener) { listeners.add(listener); MAIN.post(() -> listener.changed(snapshot())); }
    public void removeListener(Listener listener) { listeners.remove(listener); }

    public synchronized JSONObject snapshot() {
        SessionMachine.Snapshot state = machine.snapshot();
        JSONObject object = new JSONObject();
        try {
            object.put("phase", phase(state.phase)).put("generation", state.generation)
                    .put("connected", state.phase == SessionMachine.Phase.CONNECTED)
                    .put("busy", state.phase == SessionMachine.Phase.AWAITING_PERMISSION || state.phase == SessionMachine.Phase.CONNECTING || state.phase == SessionMachine.Phase.DISCONNECTING)
                    .put("scope", scope.json());
            if (!state.reason.isEmpty()) object.put("reason", state.reason);
            if (!errorMessage.isEmpty()) object.put("error", new JSONObject().put("code", state.reason).put("message", errorMessage));
        } catch (Exception ignored) { }
        return object;
    }

    public JSONObject connect() { return beginConnect(null); }
    public void connect(Consumer<String> completion) { beginConnect(completion); }

    private JSONObject beginConnect(Consumer<String> completion) {
        long ticket;
        boolean start;
        synchronized (this) {
            if (!scopeValid) return rejectedConnect(new IllegalStateException("Choose and save a valid application scope before connecting."), completion);
            boolean permission;
            try { permission = VpnService.prepare(context) != null; }
            catch (RuntimeException error) { return rejectedConnect(error, completion); }
            SessionMachine.Snapshot before = machine.snapshot();
            ticket = machine.connect(permission);
            errorMessage = "";
            if (completion != null) waiters.computeIfAbsent(ticket, key -> new ArrayList<>()).add(completion);
            completeSuperseded(ticket);
            start = ticket != before.generation && !permission;
            if (before.phase == SessionMachine.Phase.CONNECTED && completion != null) completeStatus(ticket);
        }
        publish();
        if (start) command(GopurrVpnService.CONNECT, ticket);
        return snapshot();
    }

    private JSONObject rejectedConnect(Exception error, Consumer<String> completion) {
        long ticket = machine.connect(false);
        machine.failed(ticket, "permission_failed");
        errorMessage = message(error);
        if (completion != null) MAIN.post(() -> completion.accept(failure("permission_failed", message(error))));
        publish();
        return snapshot();
    }

    public JSONObject disconnect() { return beginDisconnect("", null); }
    public void disconnect(Consumer<String> completion) { beginDisconnect("", completion); }

    private JSONObject beginDisconnect(String reason, Consumer<String> completion) {
        long ticket;
        synchronized (this) {
            ticket = machine.disconnect(reason);
            errorMessage = "";
            if (completion != null) waiters.computeIfAbsent(ticket, key -> new ArrayList<>()).add(completion);
            completeSuperseded(ticket);
        }
        // Cancellation and intent are visible before any queued work or import.
        NativeEngine.cancelPending();
        publish();
        GopurrVpnService active = service;
        if (active == null) control.execute(() -> runStop(null, ticket, 0));
        else command(GopurrVpnService.DISCONNECT, ticket);
        return snapshot();
    }

    public boolean claimPermission(long ticket) { return machine.claimPermission(ticket); }
    public void releasePermission(long ticket) { machine.releasePermission(ticket); }

    public void permissionResult(long ticket, boolean granted) {
        synchronized (this) {
            if (!machine.permission(ticket, granted)) return;
            if (!granted) {
                errorMessage = "VPN permission was not granted.";
                complete(ticket, failure("permission_denied", errorMessage));
            }
        }
        publish();
        if (granted) command(GopurrVpnService.CONNECT, ticket);
    }

    void attach(GopurrVpnService service) { this.service = service; }

    void serviceCommand(GopurrVpnService owner, String action, long ticket, int startId) {
        if (ticket == 0) {
            if (GopurrVpnService.DISCONNECT.equals(action)) disconnect();
            else connect();
            return;
        }
        if (GopurrVpnService.DISCONNECT.equals(action)) control.execute(() -> runStop(owner, ticket, startId));
        else control.execute(() -> runConnect(owner, ticket, startId));
    }

    private void command(String action, long ticket) {
        try {
            context.startForegroundService(new Intent(context, GopurrVpnService.class)
                    .setAction(action).putExtra("generation", ticket));
        } catch (RuntimeException error) {
            synchronized (this) {
                if (machine.failed(ticket, "service_failed")) { errorMessage = message(error); complete(ticket, failure("service_failed", errorMessage)); }
            }
            publish();
        }
    }

    private void runConnect(GopurrVpnService owner, long ticket, int startId) {
        if (!machine.currentConnect(ticket)) { finishIfUnwanted(owner, startId); return; }
        if (owner.destroyed()) {
            serviceFailed(owner, new IllegalStateException("The VPN service has stopped. Try connecting again."));
            return;
        }
        MAIN.postDelayed(() -> {
            synchronized (this) {
                SessionMachine.Snapshot state = machine.snapshot();
                if (state.generation != ticket || state.phase != SessionMachine.Phase.CONNECTING) return;
                machine.failed(ticket, "timeout"); errorMessage = "VPN connection timed out.";
                complete(ticket, failure("timeout", errorMessage));
            }
            NativeEngine.cancelPending(); publish();
            control.execute(() -> { cleanup(owner, ticket); finishIfUnwanted(owner, startId); });
        }, 60_000);
        try {
            // Even a superseded disconnect cannot leave an old FD under a new plan.
            stopCurrent();
            if (!machine.currentConnect(ticket) || owner.destroyed()) return;
            AppScope selected;
            synchronized (this) { selected = scope; }
            selected.verifyInstalled(context);
            NativeEngine engine = NativeEngine.get(context);
            JSONObject response = new JSONObject(engine.dispatch(new JSONObject().put("method", "engine.tunnelPlan")
                    .put("params", new JSONObject().put("appScope", selected.engineJson())).toString()));
            if (response.has("error")) throw new NativeFailure(response.getJSONObject("error"));
            TunnelPlan plan = new TunnelPlan(response.getJSONObject("result"), context.getPackageName());
            if (!plan.scope.mode.equals(selected.mode) || !plan.scope.packages.equals(selected.packages)) throw new IllegalStateException("Application scope changed while preparing the tunnel.");
            if (!machine.currentConnect(ticket) || owner.destroyed()) return;
            tunnel.assign(owner, ticket);
            String result = owner.startTunnel(plan);
            JSONObject envelope = new JSONObject(result);
            if (envelope.has("error")) throw new NativeFailure(envelope.getJSONObject("error"));
            if (!machine.connected(ticket) || owner.destroyed()) { cleanup(owner, ticket); finishIfUnwanted(owner, startId); return; }
            synchronized (this) { errorMessage = ""; complete(ticket, result); }
            publish();
            MAIN.post(() -> { if (machine.currentConnect(ticket) && !owner.destroyed()) owner.refreshNotification(snapshot()); });
        } catch (Exception error) {
            cleanup(owner, ticket);
            synchronized (this) {
                String code = error instanceof NativeFailure ? ((NativeFailure) error).code : "vpn_failed";
                if (machine.failed(ticket, code)) { errorMessage = message(error); complete(ticket, failure(code, errorMessage)); }
            }
            publish(); finishIfUnwanted(owner, startId);
        }
    }

    private void runStop(GopurrVpnService owner, long ticket, int startId) {
        SessionMachine.Snapshot state = machine.snapshot();
        if (state.generation != ticket || state.wanted) return;
        String result;
        try { result = stopCurrent(); }
        catch (Exception error) { result = failure("vpn_failed", message(error)); }
        synchronized (this) {
            if (machine.stopped(ticket)) complete(ticket, result);
        }
        publish();
        if (owner != null) finishIfUnwanted(owner, startId);
    }

    void revoked(GopurrVpnService owner) {
        long ticket;
        synchronized (this) {
            if (service != owner) return;
            ticket = machine.disconnect("permission_revoked");
            errorMessage = ""; completeSuperseded(ticket);
        }
        // Revocation removes foreground-service eligibility. Never start a
        // replacement service just to stop the revoked owner's resources.
        NativeEngine.cancelPending(); publish();
        control.execute(() -> runStop(owner, ticket, 0));
    }

    void serviceFailed(GopurrVpnService owner, Exception error) {
        long ticket;
        synchronized (this) {
            ticket = machine.snapshot().generation;
            if (service != owner) return;
            if (machine.failed(ticket, "service_failed")) {
                errorMessage = message(error); complete(ticket, failure("service_failed", errorMessage));
            }
        }
        NativeEngine.cancelPending(); publish();
        control.execute(() -> {
            GopurrVpnService owned = tunnel.takeThrough(owner, ticket);
            if (owned != null) try { owned.stopTunnel(); } catch (Exception ignored) { }
            finishIfUnwanted(owner, 0);
        });
    }

    void serviceDestroyed(GopurrVpnService owner) {
        if (service != owner) { control.execute(() -> cleanup(owner, 0)); return; }
        service = null;
        SessionMachine.Snapshot state = machine.snapshot();
        if (state.wanted) {
            synchronized (this) {
                long ticket = machine.disconnect("service_stopped");
                machine.failed(ticket, "service_stopped");
                errorMessage = "Android stopped the VPN service.";
                completeSuperseded(ticket);
            }
            NativeEngine.cancelPending(); publish();
        }
        control.execute(() -> cleanup(owner, 0));
    }

    private String stopCurrent() throws Exception {
        GopurrVpnService owner = tunnel.takeCurrent();
        return owner == null ? NativeEngine.get(context).stopVpn() : owner.stopTunnel();
    }

    private void cleanup(GopurrVpnService owner, long ticket) {
        GopurrVpnService owned = tunnel.takeIf(owner, ticket);
        if (owned != null) try { owned.stopTunnel(); } catch (Exception ignored) { }
    }

    private void finishIfUnwanted(GopurrVpnService owner, int startId) {
        MAIN.post(() -> {
            SessionMachine.Snapshot state = machine.snapshot();
            if (!state.wanted && service == owner) owner.finishStopped(startId);
        });
    }

    public synchronized JSONObject scope() { return scope.json(); }

    public synchronized JSONObject setScope(JSONObject value) throws Exception {
        SessionMachine.Phase phase = machine.snapshot().phase;
        if (phase != SessionMachine.Phase.IDLE && phase != SessionMachine.Phase.FAILED) throw new IllegalStateException("Disconnect VPN before changing application scope.");
        AppScope next = AppScope.parse(value, context.getPackageName());
        next.verifyInstalled(context);
        if (!preferences.edit().putString("scope", next.json().toString()).commit()) throw new IllegalStateException("Application scope could not be saved.");
        scope = next; scopeValid = true; errorMessage = ""; publish(); return scope.json();
    }

    public JSONObject applications() throws Exception {
        PackageManager manager = context.getPackageManager();
        Intent launcher = new Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER);
        TreeMap<String, ApplicationInfo> visible = new TreeMap<>();
        for (ResolveInfo info : manager.queryIntentActivities(launcher, 0)) {
            ApplicationInfo app = info.activityInfo.applicationInfo;
            if (!app.packageName.equals(context.getPackageName())) visible.put(app.packageName, app);
        }
        List<JSONObject> apps = new ArrayList<>();
        for (ApplicationInfo info : visible.values()) apps.add(new JSONObject().put("packageName", info.packageName)
                .put("name", manager.getApplicationLabel(info).toString()).put("system", (info.flags & ApplicationInfo.FLAG_SYSTEM) != 0));
        apps.sort(Comparator.comparing(item -> item.optString("name"), String.CASE_INSENSITIVE_ORDER));
        return new JSONObject().put("apps", new JSONArray(apps)).put("selfPackage", context.getPackageName());
    }

    private void publish() {
        MAIN.post(() -> {
            JSONObject state = snapshot();
            for (Listener listener : listeners) listener.changed(state);
        });
    }

    private synchronized void complete(long ticket, String response) {
        List<Consumer<String>> callbacks = waiters.remove(ticket);
        if (callbacks != null) for (Consumer<String> callback : callbacks) MAIN.post(() -> callback.accept(response));
    }

    private synchronized void completeSuperseded(long keep) {
        for (Long ticket : new ArrayList<>(waiters.keySet())) if (ticket != keep) complete(ticket, failure("cancelled", "The connection operation was superseded."));
    }

    private void completeStatus(long ticket) {
        NativeEngine.QUERIES.execute(() -> {
            try { complete(ticket, NativeEngine.get(context).dispatch("{\"method\":\"engine.status\"}")); }
            catch (Exception error) { complete(ticket, failure("engine_failed", message(error))); }
        });
    }

    static String failure(String code, String message) {
        try { return new JSONObject().put("error", new JSONObject().put("code", code).put("message", message)).toString(); }
        catch (Exception ignored) { return "{\"error\":{\"code\":\"internal\",\"message\":\"Native operation failed\"}}"; }
    }

    static String message(Throwable error) { return error.getMessage() == null ? "The native operation failed." : error.getMessage(); }

    private static String phase(SessionMachine.Phase phase) {
        switch (phase) {
            case AWAITING_PERMISSION: return "awaitingPermission";
            case CONNECTING: return "connecting";
            case CONNECTED: return "connected";
            case DISCONNECTING: return "disconnecting";
            case FAILED: return "failed";
            default: return "idle";
        }
    }

    private static final class NativeFailure extends Exception {
        final String code;
        NativeFailure(JSONObject error) { super(error.optString("message", "The VPN operation failed.")); code = error.optString("code", "vpn_failed"); }
    }
}
