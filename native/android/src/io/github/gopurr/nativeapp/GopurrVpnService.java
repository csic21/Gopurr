package io.github.gopurr.nativeapp;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.BroadcastReceiver;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import android.os.Handler;
import android.os.Looper;
import android.os.PowerManager;
import android.widget.RemoteViews;
import org.json.JSONObject;
import java.util.function.Consumer;

/** Owns Android resources; SessionController owns commands, generations and state. */
public final class GopurrVpnService extends VpnService {
    static final String CONNECT = "io.github.gopurr.nativeapp.CONNECT";
    static final String DISCONNECT = "io.github.gopurr.nativeapp.DISCONNECT";
    static final String CHANNEL = "gopurr-connection";
    static final String NOTIFICATION_CHANGED = "io.github.gopurr.nativeapp.NOTIFICATION_CHANGED";
    private static final int NOTIFICATION = 1;
    private volatile boolean destroyed;
    private volatile int latestStartId;
    private SessionController controller;
    // Touched only by SessionController's serial control executor.
    private ParcelFileDescriptor tun;
    private ConnectivityManager connectivity;
    private ConnectivityManager.NetworkCallback networkCallback;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final long notificationTime = System.currentTimeMillis();
    private JSONObject notificationState;
    private JSONObject traffic;
    private long notificationEpoch;
    private boolean sampling;
    private String lastNotification = "";
    private final Runnable trafficTick = this::sampleTraffic;
    private final BroadcastReceiver notificationChanges = new BroadcastReceiver() {
        @Override public void onReceive(Context context, Intent intent) {
            if (notificationState == null || destroyed) return;
            traffic = null;
            showNotification(false);
            restartTraffic();
        }
    };

    public static void connect(Context context, Consumer<String> callback) { SessionController.get(context).connect(callback); }
    public static void disconnect(Context context, Consumer<String> callback) { SessionController.get(context).disconnect(callback); }

    @Override public void onCreate() {
        super.onCreate();
        controller = SessionController.get(this);
        controller.attach(this);
        getSystemService(NotificationManager.class).createNotificationChannel(
                new NotificationChannel(CHANNEL, "Gopurr", NotificationManager.IMPORTANCE_LOW));
        IntentFilter changes = new IntentFilter(NOTIFICATION_CHANGED);
        changes.addAction(Intent.ACTION_SCREEN_ON);
        changes.addAction(Intent.ACTION_SCREEN_OFF);
        changes.addAction(PowerManager.ACTION_POWER_SAVE_MODE_CHANGED);
        if (Build.VERSION.SDK_INT >= 33) registerReceiver(notificationChanges, changes, Context.RECEIVER_NOT_EXPORTED);
        else registerReceiver(notificationChanges, changes);
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        latestStartId = startId;
        if (intent == null) { stopSelf(startId); return START_NOT_STICKY; }
        try {
            refreshNotification(controller.snapshot());
            controller.serviceCommand(this, intent.getAction(), intent.getLongExtra("generation", 0), startId);
        } catch (RuntimeException error) {
            controller.serviceFailed(this, error);
            stopSelf(startId);
        }
        return START_NOT_STICKY;
    }

    boolean destroyed() { return destroyed; }

    String startTunnel(TunnelPlan plan) throws Exception {
        if (destroyed) throw new IllegalStateException("The VPN service has stopped.");
        if (VpnService.prepare(this) != null) throw new IllegalStateException("VPN permission was revoked.");
        NativeEngine engine = NativeEngine.get(this);
        tun = plan.configure(this).establish();
        if (tun == null) throw new IllegalStateException("Android did not create the VPN interface.");
        try {
            String response = engine.startVpn(tun.getFd(), this, plan.token);
            if (new JSONObject(response).has("error")) { stopTunnel(); return response; }
            watchUnderlyingNetwork();
            return response;
        } catch (Exception error) { stopTunnel(); throw error; }
    }

    String stopTunnel() throws Exception {
        if (networkCallback != null && connectivity != null) {
            try { connectivity.unregisterNetworkCallback(networkCallback); }
            catch (RuntimeException ignored) { }
            networkCallback = null;
        }
        try { return NativeEngine.get(this).stopVpn(); }
        finally {
            if (tun != null) { tun.close(); tun = null; }
        }
    }

    private void watchUnderlyingNetwork() {
        connectivity = getSystemService(ConnectivityManager.class);
        networkCallback = new ConnectivityManager.NetworkCallback() {
            @Override public void onAvailable(Network network) { if (!destroyed) setUnderlyingNetworks(new Network[]{network}); }
            @Override public void onLost(Network network) { if (!destroyed) setUnderlyingNetworks(null); }
        };
        connectivity.registerDefaultNetworkCallback(networkCallback);
    }

    void finishStopped(int startId) {
        // A late command must not demote or stop a service handling newer work.
        if (startId != 0 && latestStartId != startId) return;
        notificationState = null;
        traffic = null;
        notificationEpoch++;
        main.removeCallbacks(trafficTick);
        lastNotification = "";
        stopForeground(STOP_FOREGROUND_REMOVE);
        if (startId == 0) stopSelf(); else stopSelf(startId);
    }

    void refreshNotification(JSONObject state) {
        if (notificationState == null || state.optLong("generation") != notificationState.optLong("generation") || !state.optBoolean("connected")) traffic = null;
        notificationState = state;
        showNotification(true);
        restartTraffic();
    }

    private boolean trafficEnabled() {
        return getSharedPreferences("android-session", MODE_PRIVATE).getBoolean("notificationTraffic", true);
    }

    private boolean canSampleTraffic() {
        NotificationManager manager = getSystemService(NotificationManager.class);
        NotificationChannel channel = manager.getNotificationChannel(CHANNEL);
        return !destroyed && notificationState != null && notificationState.optBoolean("connected")
                && trafficEnabled() && getSystemService(PowerManager.class).isInteractive()
                && manager.areNotificationsEnabled() && channel != null && channel.getImportance() != NotificationManager.IMPORTANCE_NONE;
    }

    private void restartTraffic() {
        notificationEpoch++;
        main.removeCallbacks(trafficTick);
        if (canSampleTraffic() && !sampling) main.post(trafficTick);
    }

    private void sampleTraffic() {
        if (!canSampleTraffic() || sampling) return;
        sampling = true;
        long epoch = notificationEpoch;
        long generation = notificationState.optLong("generation");
        NativeEngine.QUERIES.execute(() -> {
            JSONObject sample = null;
            try { sample = new JSONObject(NativeEngine.get(this).dispatch("{\"method\":\"engine.traffic\"}")).optJSONObject("result"); }
            catch (Exception ignored) { }
            JSONObject result = sample;
            main.post(() -> {
                sampling = false;
                JSONObject current = controller.snapshot();
                if (epoch == notificationEpoch && canSampleTraffic() && current.optBoolean("connected") && current.optLong("generation") == generation && result != null) {
                    traffic = result;
                    showNotification(false);
                }
                if (canSampleTraffic()) {
                    long delay = getSystemService(PowerManager.class).isPowerSaveMode() ? 10_000 : 2_000;
                    main.removeCallbacks(trafficTick);
                    main.postDelayed(trafficTick, delay);
                }
            });
        });
    }

    private void showNotification(boolean foreground) {
        if (notificationState == null || destroyed) return;
        JSONObject state = notificationState;
        boolean chinese = getResources().getConfiguration().getLocales().get(0).getLanguage().equals("zh");
        boolean connected = state.optBoolean("connected");
        String phase = state.optString("phase");
        String description;
        if (connected) description = chinese ? "VPN 已连接" : "VPN connected";
        else if (phase.equals("disconnecting")) description = chinese ? "正在断开 VPN…" : "Disconnecting VPN…";
        else description = chinese ? "正在连接 VPN…" : "Connecting VPN…";
        String content = description;
        String expanded = chinese ? "点击打开连接页" : "Tap to open Connect";
        if (connected && trafficEnabled() && traffic != null) {
            content = TrafficText.rates(chinese, traffic.optLong("down"), traffic.optLong("up"));
            expanded = content + "\n" + TrafficText.totals(chinese, traffic.optLong("downTotal"), traffic.optLong("upTotal"));
        } else if (connected && trafficEnabled() && !getSystemService(PowerManager.class).isInteractive()) {
            expanded = chinese ? "熄屏时暂停速率更新；亮屏后恢复。" : "Rate updates pause while the screen is off and resume when it wakes.";
        }
        String signature = content + "\n" + expanded + "\n" + description;
        if (!foreground && signature.equals(lastNotification)) return;
        PendingIntent open = PendingIntent.getActivity(this, 0,
                new Intent(this, MainActivity.class).putExtra("page", "overview").addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        PendingIntent disconnect = PendingIntent.getService(this, 1,
                new Intent(this, GopurrVpnService.class).setAction(DISCONNECT), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        RemoteViews compact = notificationContent(chinese, connected, description, open, disconnect);
        Notification.Builder builder = new Notification.Builder(this, CHANNEL).setSmallIcon(R.drawable.notification_icon)
                .setContentTitle("Gopurr").setContentText(content).setSubText(description).setContentIntent(open)
                .setStyle(new Notification.DecoratedCustomViewStyle())
                .setCustomContentView(compact).setCustomBigContentView(compact.clone())
                .setCategory(Notification.CATEGORY_SERVICE).setWhen(notificationTime).setShowWhen(false)
                .setOngoing(true).setOnlyAlertOnce(true);
        if (Build.VERSION.SDK_INT >= 31) builder.setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE);
        Notification notice = builder.build();
        if (!foreground) getSystemService(NotificationManager.class).notify(NOTIFICATION, notice);
        else if (Build.VERSION.SDK_INT >= 34) startForeground(NOTIFICATION, notice, ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED);
        else startForeground(NOTIFICATION, notice);
        lastNotification = signature;
    }

    private RemoteViews notificationContent(boolean chinese, boolean connected, String description,
                                            PendingIntent open, PendingIntent disconnect) {
        RemoteViews view = new RemoteViews(getPackageName(), R.layout.notification_compact);
        String down = description;
        String up = chinese ? "点击打开连接页" : "Tap to open Connect";
        if (connected && trafficEnabled() && traffic != null) {
            down = (chinese ? "下载 " : "Down ") + TrafficText.bytes(traffic.optLong("down")) + "/s";
            up = (chinese ? "上传 " : "Up ") + TrafficText.bytes(traffic.optLong("up")) + "/s";
            down += " · " + TrafficText.bytes(traffic.optLong("downTotal"));
            up += " · " + TrafficText.bytes(traffic.optLong("upTotal"));
        } else if (connected && trafficEnabled() && !getSystemService(PowerManager.class).isInteractive()) {
            up = chinese ? "熄屏暂停速率更新" : "Rate updates paused";
        }
        view.setTextViewText(R.id.notification_down, down);
        view.setTextViewText(R.id.notification_up, up);
        view.setContentDescription(R.id.notification_down, down);
        view.setContentDescription(R.id.notification_up, up);
        view.setTextViewText(R.id.notification_disconnect, chinese ? "断开连接" : "Disconnect");
        view.setOnClickPendingIntent(R.id.notification_body, open);
        view.setOnClickPendingIntent(R.id.notification_disconnect, disconnect);
        return view;
    }

    @Override public void onRevoke() {
        destroyed = true;
        NativeEngine.cancelPending();
        controller.revoked(this);
        super.onRevoke();
    }

    @Override public void onDestroy() {
        destroyed = true;
        notificationEpoch++;
        main.removeCallbacks(trafficTick);
        unregisterReceiver(notificationChanges);
        if (controller != null) controller.serviceDestroyed(this);
        super.onDestroy();
    }
}
