package io.github.gopurr.nativeapp;

import android.app.Activity;
import android.content.Intent;
import android.os.Build;
import android.os.Bundle;
import android.widget.TextView;
import com.longbridge.nsh.NshHostReply;
import com.longbridge.nsh.NshView;
import org.json.JSONObject;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.ExecutorService;

/** A replaceable NSH view and system-dialog presenter; connection intent lives in SessionController. */
public final class MainActivity extends Activity {
    private NshView app;
    private SessionController controller;
    private NativeActions actions;
    private final Map<Integer, Long> permissionRequests = new HashMap<>();
    private int nextPermissionCode = 1024;
    private SessionController.Listener listener;
    private boolean resumed;
    private String pendingPage;

    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        if (Build.VERSION.SDK_INT >= 30) getWindow().setDecorFitsSystemWindows(false);
        controller = SessionController.get(this);
        if (state != null) {
            int[] codes = state.getIntArray("permissionCodes");
            long[] tickets = state.getLongArray("permissionTickets");
            if (codes != null && tickets != null && codes.length == tickets.length) {
                for (int i = 0; i < codes.length; i++) permissionRequests.put(codes[i], tickets[i]);
            }
            nextPermissionCode = state.getInt("nextPermissionCode", 1024);
        }
        app = new NshView(this);
        app.setStorageDirectory(new java.io.File(getFilesDir(), "ui"));
        app.setLimits(128, 0, 10_000, 2_000, 50_000);
        app.setErrorListener(this::showFailure);
        actions = new NativeActions(this);
        try {
            JSONObject params = new JSONObject().put("nativeHost", true).put("android", true).put("mobileHost", true)
                    .put("engine", new JSONObject().put("transport", "native").put("vpnAvailable", true));
            String page = getIntent().getStringExtra("page");
            if ("overview".equals(page) || "profiles".equals(page) || "proxies".equals(page) || "settings".equals(page)) params.put("page", page);
            app.setParams(params);
            if (!app.setGrantedCapabilities("{\"host\":[\"engine.dispatch\",\"android.session\",\"android.connect\",\"android.disconnect\",\"android.apps\",\"android.scope.get\",\"android.scope.set\",\"android.file.pick\",\"android.notifications\",\"android.notifications.request\",\"android.notifications.settings\",\"android.notifications.configure\",\"android.appearance\"],\"statusBar\":true}")) {
                throw new IllegalStateException("The native host permissions could not be configured.");
            }
            app.handle("android.session", (data, reply) -> reply.resolve(controller.snapshot()));
            app.handle("android.connect", (data, reply) -> reply.resolve(controller.connect()));
            app.handle("android.disconnect", (data, reply) -> reply.resolve(controller.disconnect()));
            app.handle("android.scope.get", (data, reply) -> reply.resolve(controller.scope()));
            app.handle("android.file.pick", (data, reply) -> actions.pickFile(reply));
            app.handle("android.notifications", (data, reply) -> reply.resolve(actions.notifications()));
            app.handle("android.notifications.request", (data, reply) -> actions.requestNotifications(reply));
            app.handle("android.notifications.settings", (data, reply) -> {
                try { reply.resolve(actions.openNotificationSettings()); }
                catch (Exception error) { reply.reject(SessionController.message(error)); }
            });
            app.handle("android.notifications.configure", (data, reply) -> {
                try { reply.resolve(actions.configureNotifications(object(data))); }
                catch (Exception error) { reply.reject(SessionController.message(error)); }
            });
            app.handle("android.appearance", (data, reply) -> {
                try { reply.resolve(actions.appearance(object(data))); }
                catch (Exception error) { reply.reject(SessionController.message(error)); }
            });
            app.handle("android.scope.set", (data, reply) -> {
                try { reply.resolve(controller.setScope(object(data))); }
                catch (Exception error) { reply.reject(SessionController.message(error)); }
            });
            app.handle("android.apps", (data, reply) -> NativeEngine.WORKER.execute(() -> {
                try { reply.resolve(controller.applications()); }
                catch (Exception error) { reply.reject(SessionController.message(error)); }
            }));
            app.handle("engine.dispatch", (data, reply) -> {
                try {
                    JSONObject request = object(data);
                    String method = request.optString("method");
                    if ("engine.start".equals(method)) controller.connect(response -> resolve(reply, response));
                    else if ("engine.stop".equals(method)) controller.disconnect(response -> resolve(reply, response));
                    else {
                        ExecutorService executor = readMethod(method) ? NativeEngine.QUERIES : NativeEngine.WORKER;
                        executor.execute(() -> {
                            try { resolve(reply, NativeEngine.get(this).dispatch(request.toString())); }
                            catch (Exception error) { reply.reject(SessionController.message(error)); }
                        });
                    }
                } catch (Exception error) { reply.reject(SessionController.message(error)); }
            });
            listener = snapshot -> { if (app != null) { app.send("android.session", snapshot); presentPermission(snapshot); } };
            controller.addListener(listener);
            setContentView(app);
            if (!app.loadAssetBundle("nsh")) showFailure("The Gopurr interface bundle could not be loaded.");
        } catch (Exception error) { showFailure(SessionController.message(error)); }
    }

    private void presentPermission(JSONObject state) {
        long ticket = state.optLong("generation");
        boolean waiting = state.optString("phase").equals("awaitingPermission");
        for (Map.Entry<Integer, Long> request : new HashMap<>(permissionRequests).entrySet()) {
            if (!waiting || request.getValue() != ticket) finishActivity(request.getKey());
        }
        if (!waiting || permissionRequests.containsValue(ticket) || !controller.claimPermission(ticket)) return;
        Intent permission = android.net.VpnService.prepare(this);
        if (permission == null) { controller.permissionResult(ticket, true); return; }
        int code = nextPermissionCode++;
        if (nextPermissionCode > 16_000) nextPermissionCode = 1024;
        while (permissionRequests.containsKey(code)) code = nextPermissionCode++;
        permissionRequests.put(code, ticket);
        try { startActivityForResult(permission, code); }
        catch (RuntimeException error) { permissionRequests.remove(code); controller.permissionResult(ticket, false); }
    }

    @Override protected void onActivityResult(int request, int result, Intent data) {
        super.onActivityResult(request, result, data);
        if (actions != null && actions.activityResult(request, result, data)) return;
        Long ticket = permissionRequests.remove(request);
        if (ticket != null) controller.permissionResult(ticket, result == RESULT_OK);
        presentPermission(controller.snapshot());
    }

    @Override public void onRequestPermissionsResult(int request, String[] permissions, int[] results) {
        super.onRequestPermissionsResult(request, permissions, results);
        if (actions != null) actions.permissionsResult(request);
    }

    @Override protected void onSaveInstanceState(Bundle state) {
        int[] codes = new int[permissionRequests.size()];
        long[] tickets = new long[codes.length]; int index = 0;
        for (Map.Entry<Integer, Long> request : permissionRequests.entrySet()) {
            codes[index] = request.getKey(); tickets[index] = request.getValue(); index++;
        }
        state.putIntArray("permissionCodes", codes); state.putLongArray("permissionTickets", tickets);
        state.putInt("nextPermissionCode", nextPermissionCode);
        super.onSaveInstanceState(state);
    }

    private static boolean readMethod(String method) {
        return method.equals("engine.status") || method.equals("engine.traffic") || method.endsWith(".list") || method.endsWith(".get") || method.equals("proxies.groups");
    }

    private static JSONObject object(Object data) throws Exception {
        if (data instanceof JSONObject) return (JSONObject) data;
        if (data instanceof Map) return new JSONObject((Map<?, ?>) data);
        throw new IllegalArgumentException("A native request must be a JSON object.");
    }

    private static void resolve(NshHostReply reply, String response) {
        try { reply.resolve(new JSONObject(response)); }
        catch (Exception error) { reply.reject("The native engine returned an invalid response."); }
    }

    private void showFailure(String message) {
        TextView text = new TextView(this);
        text.setText("Gopurr\n\n" + message); text.setTextSize(16); text.setPadding(32, 64, 32, 32);
        text.setTextColor(android.graphics.Color.BLACK); text.setBackgroundColor(android.graphics.Color.WHITE);
        setContentView(text);
    }

    @Override public void onBackPressed() { if (app == null || !app.handleBack()) super.onBackPressed(); }

    @Override protected void onResume() {
        super.onResume();
        resumed = true;
        if (actions != null) { actions.applyAppearance(); actions.refreshNotifications(); }
        // The SDK publishes its active lifecycle after Activity.onResume.
        // Deliver navigation on the next main-loop turn, after that transition.
        getWindow().getDecorView().post(this::deliverNavigation);
    }

    @Override protected void onPause() {
        resumed = false;
        super.onPause();
    }

    @Override protected void onNewIntent(Intent intent) {
        super.onNewIntent(intent);
        setIntent(intent);
        String page = intent.getStringExtra("page");
        if ("overview".equals(page) || "profiles".equals(page) || "proxies".equals(page) || "settings".equals(page)) {
            pendingPage = page;
            getWindow().getDecorView().post(this::deliverNavigation);
        }
    }

    private void deliverNavigation() {
        if (app != null && resumed && pendingPage != null) {
            try { if (app.send("android.navigate", new JSONObject().put("page", pendingPage))) pendingPage = null; }
            catch (Exception ignored) { }
        }
    }

    @Override protected void onDestroy() {
        if (listener != null) controller.removeListener(listener);
        if (actions != null) actions.close();
        if (isFinishing()) for (long ticket : permissionRequests.values()) controller.releasePermission(ticket);
        if (app != null) { app.destroy(); app = null; }
        // Closing or recreating a view never changes the process-owned session.
        super.onDestroy();
    }
}
