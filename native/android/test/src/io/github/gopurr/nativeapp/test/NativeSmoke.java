package io.github.gopurr.nativeapp.test;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Context;
import android.content.Intent;
import android.content.BroadcastReceiver;
import android.content.IntentFilter;
import android.content.ComponentName;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.graphics.Bitmap;
import android.view.accessibility.AccessibilityNodeInfo;

import io.github.gopurr.nativeapp.GopurrVpnService;
import io.github.gopurr.nativeapp.MainActivity;
import io.github.gopurr.nativeapp.NativeEngine;
import io.github.gopurr.nativeapp.SessionController;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/** Emulator-only instrumentation. No test hooks are shipped in the main APK. */
public final class NativeSmoke extends Instrumentation {
    private static final String FIXTURE = "proxies: []\n"
            + "log-level: info\n"
            + "proxy-groups:\n"
            + "  - name: Native smoke\n"
            + "    type: select\n"
            + "    proxies: [DIRECT, REJECT]\n"
            + "rules:\n"
            + "  - MATCH,Native smoke\n";
    private static final String MARKER = "GOPURR_ANDROID_NATIVE_SMOKE";
    private Bundle arguments;
    private NativeEngine engine;
    private Activity activity;
    private int hostPort;
    private String lastProbeNetwork;
    private UiAutomation automation;

    @Override public void onCreate(Bundle arguments) {
        super.onCreate(arguments);
        this.arguments = arguments;
        start();
    }

    @Override public void onStart() {
        Bundle result = new Bundle();
        try {
            hostPort = Integer.parseInt(arguments.getString("hostPort", "0"));
            require(hostPort > 0 && hostPort <= 65535, "A local HTTP fixture port is required");
            Context context = getTargetContext();
            require("io.github.gopurr.nativeapp".equals(context.getPackageName()), "Wrong instrumentation target");
            Intent launch = new Intent(context, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
            activity = startActivitySync(launch);
            waitForIdleSync();
            automation = getUiAutomation();
            engine = NativeEngine.get(context);

            result.putInt("processId", android.os.Process.myPid());
            if ("true".equals(arguments.getString("restart"))) {
                verifyProcessRestart();
                result.putString("restartSmoke", "PASS: new process retained routing mode and profile node; real VPN DIRECT/REJECT routing verified");
                finish(Activity.RESULT_OK, result);
                return;
            }

            JSONObject initial = call("engine.status", null);
            require(initial.getBoolean("vpnSupported"), "Android VPN bridge is unavailable");
            require(!initial.getBoolean("tun"), "Test app must start with VPN disconnected");

            JSONObject imported = call("profiles.import", new JSONObject().put("name", "Native smoke").put("yaml", FIXTURE));
            String id = imported.getString("id");
            require(!id.isEmpty(), "JNI import returned no profile ID");
            JSONObject active = call("profiles.activate", new JSONObject().put("id", id));
            require(id.equals(active.getString("activeProfile")), "Imported profile was not selected");
            call("engine.stop", null);
            require(!call("engine.status", null).getBoolean("running"), "Local core did not stop before VPN start");
            cancelledPermissionCannotReconnect();
            statusDuringDownload();
            validateScope();
            require(probeHTTP("baseline").contains(MARKER), "Emulator could not reach the local fixture before VPN startup");

            JSONObject connected = vpn(true);
            require(connected.getBoolean("tun") && connected.getBoolean("running"), "Service did not attach the real TUN");
            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
            require(probeHTTP("direct").contains(MARKER), "DIRECT traffic did not reach the local fixture through Android VPN");

            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
            require(!probeHTTP("reject").contains(MARKER), "REJECT traffic escaped the VPN rules");
            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
            require(probeHTTP("restored").contains(MARKER), "DIRECT traffic did not recover after node switching");

            JSONArray logs = call("logs.list", null).getJSONArray("logs");
            boolean sawTun = false;
            for (int i = 0; i < logs.length(); i++) {
                String text = logs.getJSONObject(i).optString("text");
                if (text.contains("TUN") || text.contains("Tun adapter")) sawTun = true;
            }
            require(sawTun, "No real TUN startup event was captured");
            JSONObject traffic = call("engine.status", null).getJSONObject("traffic");
            require(traffic.getLong("upTotal") > 0 && traffic.getLong("downTotal") > 0, "No real packet accounting was recorded");

            JSONObject stopped = vpn(false);
            require(!stopped.getBoolean("tun") && !stopped.getBoolean("running"), "VPN did not stop");
            require(!call("engine.status", null).getBoolean("tun"), "Status kept a stale connected TUN");
            // Service.onDestroy is queued separately from the disconnect reply.
            // Observe service disappearance, rather than relying on an arbitrary
            // sleep which could hide descriptor reuse/reconnection problems.
            awaitServiceStopped();
            JSONObject reconnected = vpn(true);
            require(reconnected.getBoolean("tun"), "VPN could not reconnect after descriptor release");
            require(probeHTTP("reconnected").contains(MARKER), "Reconnected VPN did not forward packets");
            rapidReconnect();
            require(shell("dumpsys activity services io.github.gopurr.nativeapp").contains("isForeground=true"), "Rapid reconnect lost foreground service protection");
            require(probeHTTP("rapid-reconnected").contains(MARKER), "Rapidly reconnected VPN did not forward packets");
            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
            require(!probeHTTP("rapid-reject").contains(MARKER), "Rapidly reconnected VPN did not apply REJECT");
            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
            require(probeHTTP("rapid-restored").contains(MARKER), "Rapidly reconnected VPN did not recover DIRECT");
            applicationScopeRouting();
            selectedNodeSurvivesReconnect();
            if ("true".equals(arguments.getString("capture"))) captureOverview(true);
            if ("true".equals(arguments.getString("capture"))) { openPage("proxies"); capture("native-smoke-proxies.png"); }
            require(!vpn(false).getBoolean("tun"), "Final VPN stop failed");
            if ("true".equals(arguments.getString("capture"))) captureOverview(false);
            if ("true".equals(arguments.getString("capture"))) captureMobilePages();
            // Leave an unmistakable pair of preferences for the runner's next
            // instrumentation process. It force-stops this UID between phases.
            call("engine.start", null);
            call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
            call("engine.mode", new JSONObject().put("mode", "direct"));
            call("engine.stop", null);
            result.putString("nativeSmoke", "PASS: JNI import, tunnel plan, real TUN, DIRECT/REJECT, traffic, stop/reconnect, selected-node restore, stale consent, fast status and application scopes");
            finish(Activity.RESULT_OK, result);
        } catch (Throwable failure) {
            try { if (engine != null) result.putString("engineLogs", call("logs.list", null).toString()); } catch (Throwable ignored) { }
            try { if (engine != null) vpn(false); } catch (Throwable ignored) { }
            result.putString("nativeSmoke", "FAIL: " + failure.getClass().getSimpleName() + ": " + failure.getMessage());
            finish(Activity.RESULT_CANCELED, result);
        }
    }

    private void verifyProcessRestart() throws Exception {
        JSONObject status = call("engine.status", null);
        require(!status.getBoolean("running") && !status.getBoolean("tun"), "Process restart connected without an explicit request");
        require("direct".equals(status.getString("mode")), "Process restart lost the saved routing mode");
        require(!status.getString("activeProfile").isEmpty(), "Process restart lost its active subscription");
        require(vpn(true).getBoolean("tun"), "VPN could not connect after process restart");
        JSONArray groups = call("proxies.groups", null).getJSONArray("groups");
        boolean restored = false;
        for (int i = 0; i < groups.length(); i++) {
            JSONObject group = groups.getJSONObject(i);
            if (group.optString("name").equals("Native smoke")) restored = group.optString("now").equals("REJECT");
        }
        require(restored, "Process restart lost the profile's saved REJECT choice");
        require(probeHTTP("restart-mode-direct").contains(MARKER), "Saved direct mode was not applied to the real VPN");
        call("engine.mode", new JSONObject().put("mode", "rule"));
        require(!probeHTTP("restart-reject").contains(MARKER), "Saved node did not reject packets after process restart");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
        require(probeHTTP("restart-restored").contains(MARKER), "VPN did not forward packets after changing the restored node");
        require(!vpn(false).getBoolean("tun"), "Restart verification left a connected VPN");
    }

    private JSONObject call(String method, JSONObject parameters) throws Exception {
        JSONObject request = new JSONObject().put("method", method);
        if (parameters != null) request.put("params", parameters);
        return unwrap(engine.dispatch(request.toString()));
    }

    private JSONObject vpn(boolean connect) throws Exception {
        CountDownLatch completed = new CountDownLatch(1);
        AtomicReference<String> reply = new AtomicReference<>();
        runOnMainSync(() -> {
            java.util.function.Consumer<String> callback = response -> { reply.set(response); completed.countDown(); };
            if (connect) GopurrVpnService.connect(getTargetContext(), callback);
            else GopurrVpnService.disconnect(getTargetContext(), callback);
        });
        require(completed.await(65, TimeUnit.SECONDS), "VPN service did not answer");
        return unwrap(reply.get());
    }

    private JSONObject unwrap(String response) throws Exception {
        JSONObject envelope = new JSONObject(response);
        if (envelope.has("error")) {
            JSONObject error = envelope.getJSONObject("error");
            throw new IllegalStateException(error.optString("code") + ": " + error.optString("message"));
        }
        return envelope.getJSONObject("result");
    }

    private void rapidReconnect() throws Exception {
        CountDownLatch completed = new CountDownLatch(2);
        AtomicReference<String> connectReply = new AtomicReference<>();
        AtomicReference<String> disconnectReply = new AtomicReference<>();
        runOnMainSync(() -> {
            GopurrVpnService.disconnect(getTargetContext(), response -> { disconnectReply.set(response); completed.countDown(); });
            GopurrVpnService.connect(getTargetContext(), response -> { connectReply.set(response); completed.countDown(); });
        });
        require(completed.await(65, TimeUnit.SECONDS), "Rapid reconnect did not finish both commands");
        JSONObject disconnect = new JSONObject(disconnectReply.get());
        require(!disconnect.has("error") || disconnect.getJSONObject("error").optString("code").equals("cancelled"), "Unexpected rapid disconnect error");
        require(unwrap(connectReply.get()).getBoolean("tun"), "Rapid reconnect did not attach TUN");
        require(call("engine.status", null).getBoolean("tun"), "An old disconnect stopped the newer connection");
    }

    private void captureOverview(boolean connected) throws Exception {
        require(call("engine.status", null).getBoolean("tun") == connected, "Engine state changed before capture");
        // Reopening the real Activity exercises the foreground restoration
        // path and requests a fresh snapshot, rather than photographing an
        // earlier view before its periodic two-second refresh has arrived.
        activity = startActivitySync(new Intent(getTargetContext(), MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK));
        waitForIdleSync();
        // NSH's native view tree is deeper than a conventional Android form.
        // Capture the actual settled window; visual QA reviews its pixels.
        automation.waitForIdle(300, 5000);
        Bitmap bitmap = automation.takeScreenshot();
        require(bitmap != null, "Android could not capture its native window");
        File file = new File(getTargetContext().getCacheDir(), connected ? "native-smoke-overview-live.png" : "native-smoke-overview-disconnected.png");
        try (FileOutputStream output = new FileOutputStream(file)) {
            require(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output), "Could not encode overview screenshot");
        } finally { bitmap.recycle(); }
    }

    private String probeHTTP(String phase) throws Exception {
        // Both Gopurr's own UID and Android's privileged shell can bypass the
        // VPN. Run this socket in the separate test package's ordinary UID.
        Context context = getTargetContext();
        String action = "io.github.gopurr.nativeapp.PROBE_" + java.util.UUID.randomUUID().toString();
        CountDownLatch completed = new CountDownLatch(1);
        AtomicReference<String> response = new AtomicReference<>("");
        AtomicReference<String> error = new AtomicReference<>("");
        AtomicReference<String> network = new AtomicReference<>("");
        java.util.concurrent.atomic.AtomicInteger uid = new java.util.concurrent.atomic.AtomicInteger(-1);
        BroadcastReceiver receiver = new BroadcastReceiver() {
            @Override public void onReceive(Context unused, Intent result) {
                response.set(result.getStringExtra("response"));
                error.set(result.getStringExtra("error"));
                network.set(result.getStringExtra("network"));
                uid.set(result.getIntExtra("uid", -1));
                completed.countDown();
            }
        };
        if (android.os.Build.VERSION.SDK_INT >= 33) context.registerReceiver(receiver, new IntentFilter(action), Context.RECEIVER_EXPORTED);
        else context.registerReceiver(receiver, new IntentFilter(action));
        try {
            Intent probe = new Intent().setComponent(new ComponentName("io.github.gopurr.nativeapp.test", "io.github.gopurr.nativeapp.test.ProbeActivity"))
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK).putExtra("replyAction", action).putExtra("phase", phase).putExtra("hostPort", hostPort);
            if (phase.equals("excluded-bypass")) probe.putExtra("expectedVpn", false);
            // Full service shutdown must remove its network. Same-service rapid
            // handover may retain Android's NetworkAgent identity; verify its
            // real DIRECT/REJECT data path instead of requiring a different ID.
            // AOSP Vpn.establish: updateLinkPropertiesInPlaceIfPossible.
            if ((phase.equals("reconnected") || phase.equals("sticky-reject")) && lastProbeNetwork != null) probe.putExtra("previousNetwork", lastProbeNetwork);
            runOnMainSync(() -> context.startActivity(probe));
            require(completed.await(12, TimeUnit.SECONDS), "Independent HTTP probe did not reply for " + phase);
            require(uid.get() > 10000 && uid.get() != android.os.Process.myUid(), "HTTP probe did not use an independent normal application UID");
            lastProbeNetwork = network.get();
            if (!response.get().contains(MARKER) && !phase.endsWith("reject")) {
                throw new AssertionError("Independent " + phase + " probe failed: " + error.get());
            }
            return response.get();
        } finally {
            context.unregisterReceiver(receiver);
            activity = startActivitySync(new Intent(context, MainActivity.class).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK));
            waitForIdleSync();
        }
    }

    private void captureMobilePages() throws Exception {
        openPage("profiles"); capture("native-smoke-profiles.png");
        openPage("settings"); capture("native-smoke-settings.png");
        press("open-app-scope", false);
        press("Selected only", true);
        press("scope-app-com.google.android.calendar", false);
        waitForText("1 apps selected");
        capture("native-smoke-app-scope.png");
    }

    private void openPage(String page) throws Exception {
        activity = startActivitySync(new Intent(getTargetContext(), MainActivity.class)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK).putExtra("page", page));
        waitForIdleSync();
        automation.waitForIdle(300, 5000);
    }

    private void capture(String name) throws Exception {
        automation.waitForIdle(300, 5000);
        Bitmap bitmap = automation.takeScreenshot();
        require(bitmap != null, "Android screenshot was unavailable");
        try (FileOutputStream output = new FileOutputStream(new File(getTargetContext().getCacheDir(), name))) {
            require(bitmap.compress(Bitmap.CompressFormat.PNG, 100, output), "Android screenshot could not be encoded");
        } finally { bitmap.recycle(); }
    }

    private void press(String value, boolean text) throws Exception {
        long deadline = android.os.SystemClock.elapsedRealtime() + 5000;
        while (android.os.SystemClock.elapsedRealtime() < deadline) {
            java.util.ArrayDeque<AccessibilityNodeInfo> queue = new java.util.ArrayDeque<>();
            AccessibilityNodeInfo root = automation.getRootInActiveWindow();
            if (root != null) queue.add(root);
            int inspected = 0;
            while (!queue.isEmpty() && inspected++ < 5000) {
                AccessibilityNodeInfo node = queue.remove();
                String actual = text ? String.valueOf(node.getText()) : node.getViewIdResourceName();
                if (value.equals(actual) && node.isEnabled()) {
                    AccessibilityNodeInfo target = node;
                    for (int depth = 0; target != null && depth < 8; depth++, target = target.getParent()) {
                        if (target.isEnabled() && target.isClickable() && target.performAction(AccessibilityNodeInfo.ACTION_CLICK)) {
                            automation.waitForIdle(300, 5000); return;
                        }
                    }
                }
                for (int i = 0; i < node.getChildCount(); i++) { AccessibilityNodeInfo child = node.getChild(i); if (child != null) queue.add(child); }
            }
            android.os.SystemClock.sleep(25);
        }
        throw new AssertionError("Missing Android control: " + value);
    }

    private void waitForText(String value) throws Exception {
        long deadline = android.os.SystemClock.elapsedRealtime() + 5000;
        while (android.os.SystemClock.elapsedRealtime() < deadline) {
            java.util.ArrayDeque<AccessibilityNodeInfo> queue = new java.util.ArrayDeque<>();
            AccessibilityNodeInfo root = automation.getRootInActiveWindow();
            if (root != null) queue.add(root);
            int count = 0;
            while (!queue.isEmpty() && count++ < 5000) {
                AccessibilityNodeInfo node = queue.remove();
                if (value.equals(String.valueOf(node.getText()))) return;
                for (int i = 0; i < node.getChildCount(); i++) { AccessibilityNodeInfo child = node.getChild(i); if (child != null) queue.add(child); }
            }
            android.os.SystemClock.sleep(25);
        }
        throw new AssertionError("Android did not show: " + value);
    }

    private void selectedNodeSurvivesReconnect() throws Exception {
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
        vpn(false); awaitServiceStopped();
        require(vpn(true).getBoolean("tun"), "VPN reconnect failed during selection restoration");
        JSONArray groups = call("proxies.groups", null).getJSONArray("groups");
        boolean selected = false;
        for (int i = 0; i < groups.length(); i++) {
            JSONObject group = groups.getJSONObject(i);
            if (group.optString("name").equals("Native smoke")) selected = group.optString("now").equals("REJECT");
        }
        require(selected, "A manually selected node was lost across VPN reconnect");
        require(!probeHTTP("sticky-reject").contains(MARKER), "Restored REJECT node did not reject packets");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
        require(probeHTTP("sticky-restored").contains(MARKER), "Packets did not recover after restoring DIRECT");
    }

    private void cancelledPermissionCannotReconnect() throws Exception {
        shell("appops set io.github.gopurr.nativeapp ACTIVATE_VPN ignore");
        SessionController controller = SessionController.get(getTargetContext());
        AtomicReference<JSONObject> pending = new AtomicReference<>();
        runOnMainSync(() -> pending.set(controller.connect()));
        require(pending.get().optString("phase").equals("awaitingPermission"), "Consent must be owned by the pending session");
        long ticket = pending.get().getLong("generation");
        runOnMainSync(() -> { controller.disconnect(); controller.permissionResult(ticket, true); });
        awaitPhase("idle");
        require(!call("engine.status", null).getBoolean("tun"), "A late consent result restarted the VPN");
        shell("appops set io.github.gopurr.nativeapp ACTIVATE_VPN allow");
    }

    private void statusDuringDownload() throws Exception {
        CountDownLatch finished = new CountDownLatch(1);
        Thread download = new Thread(() -> {
            try { call("profiles.import", new JSONObject().put("name", "Slow fixture").put("url", "http://10.0.2.2:" + hostPort + "/native-smoke/slow-profile.yaml")); }
            catch (Exception ignored) { }
            finally { finished.countDown(); }
        });
        download.start();
        long deadline = android.os.SystemClock.elapsedRealtime() + 4000;
        boolean busy = false;
        while (android.os.SystemClock.elapsedRealtime() < deadline && finished.getCount() != 0) {
            if (call("engine.status", null).optBoolean("busy")) { busy = true; break; }
            android.os.SystemClock.sleep(10);
        }
        require(busy, "The delayed import did not enter its pending operation");
        long started = android.os.SystemClock.elapsedRealtime();
        require(call("engine.status", null).optBoolean("busy"), "Status lost its pending import");
        require(android.os.SystemClock.elapsedRealtime() - started < 500, "Engine status waited for a profile download");
        started = android.os.SystemClock.elapsedRealtime();
        SessionController.get(getTargetContext()).snapshot();
        require(android.os.SystemClock.elapsedRealtime() - started < 200, "Native session snapshot waited for a download");
        vpn(false);
        require(finished.await(2, TimeUnit.SECONDS), "Disconnect did not cancel a pending download");
    }

    private void validateScope() throws Exception {
        SessionController controller = SessionController.get(getTargetContext());
        boolean rejected = false;
        try { controller.setScope(new JSONObject().put("mode", "include").put("packages", new JSONArray())); }
        catch (IllegalArgumentException expected) { rejected = true; }
        require(rejected, "An empty include list captured every application");
        rejected = false;
        try { controller.setScope(new JSONObject().put("mode", "include").put("packages", new JSONArray().put(getTargetContext().getPackageName()))); }
        catch (IllegalArgumentException expected) { rejected = true; }
        require(rejected, "Gopurr was allowed to include its own UID");
    }

    private void applicationScopeRouting() throws Exception {
        SessionController controller = SessionController.get(getTargetContext());
        boolean refused = false;
        try { controller.setScope(new JSONObject().put("mode", "all")); }
        catch (IllegalStateException expected) { refused = true; }
        require(refused, "Application scope changed under an active TUN");
        vpn(false); awaitServiceStopped();
        String probe = "io.github.gopurr.nativeapp.test";
        controller.setScope(new JSONObject().put("mode", "include").put("packages", new JSONArray().put(probe)));
        require(vpn(true).getBoolean("tun"), "Include scope failed to connect");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
        require(probeHTTP("include-direct").contains(MARKER), "Included application could not forward traffic");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
        require(!probeHTTP("include-reject").contains(MARKER), "Included application bypassed the proxy rules");
        vpn(false); awaitServiceStopped();
        controller.setScope(new JSONObject().put("mode", "exclude").put("packages", new JSONArray().put(probe)));
        require(vpn(true).getBoolean("tun"), "Exclude scope failed to connect");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "REJECT"));
        require(probeHTTP("excluded-bypass").contains(MARKER), "Excluded application was still captured by VPN");
        vpn(false); awaitServiceStopped();
        controller.setScope(new JSONObject().put("mode", "all").put("packages", new JSONArray()));
        require(vpn(true).getBoolean("tun"), "All-app scope could not be restored");
        call("proxies.select", new JSONObject().put("group", "Native smoke").put("member", "DIRECT"));
        require(probeHTTP("all-restored").contains(MARKER), "All-app scope did not restore the probe's VPN route");
    }

    private void awaitPhase(String phase) throws Exception {
        long deadline = android.os.SystemClock.elapsedRealtime() + 10_000;
        while (android.os.SystemClock.elapsedRealtime() < deadline) {
            if (SessionController.get(getTargetContext()).snapshot().optString("phase").equals(phase)) return;
            android.os.SystemClock.sleep(10);
        }
        throw new AssertionError("Native session did not reach " + phase);
    }

    private String shell(String command) throws Exception {
        try (ParcelFileDescriptor.AutoCloseInputStream input = new ParcelFileDescriptor.AutoCloseInputStream(automation.executeShellCommand(command));
             ByteArrayOutputStream output = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[4096];
            int count;
            while ((count = input.read(buffer)) != -1) output.write(buffer, 0, count);
            return output.toString(StandardCharsets.UTF_8.name());
        }
    }

    private void awaitServiceStopped() throws Exception {
        long deadline = android.os.SystemClock.elapsedRealtime() + 10_000;
        while (android.os.SystemClock.elapsedRealtime() < deadline) {
            String services = shell("dumpsys activity services io.github.gopurr.nativeapp");
            if (!services.contains("ServiceRecord") || !services.contains("GopurrVpnService")) return;
            android.os.SystemClock.sleep(50);
        }
        throw new IllegalStateException("VPN service did not leave the activity service registry");
    }

    private static void require(boolean condition, String message) {
        if (!condition) throw new AssertionError(message);
    }
}
