package io.github.gopurr.nativeapp;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.VpnService;
import android.os.Build;
import android.os.ParcelFileDescriptor;
import org.json.JSONObject;
import java.util.function.Consumer;

/** Owns Android resources; SessionController owns commands, generations and state. */
public final class GopurrVpnService extends VpnService {
    static final String CONNECT = "io.github.gopurr.nativeapp.CONNECT";
    static final String DISCONNECT = "io.github.gopurr.nativeapp.DISCONNECT";
    private static final String CHANNEL = "gopurr-connection";
    private static final int NOTIFICATION = 1;
    private volatile boolean destroyed;
    private volatile int latestStartId;
    private SessionController controller;
    // Touched only by SessionController's serial control executor.
    private ParcelFileDescriptor tun;
    private ConnectivityManager connectivity;
    private ConnectivityManager.NetworkCallback networkCallback;

    public static void connect(Context context, Consumer<String> callback) { SessionController.get(context).connect(callback); }
    public static void disconnect(Context context, Consumer<String> callback) { SessionController.get(context).disconnect(callback); }

    @Override public void onCreate() {
        super.onCreate();
        controller = SessionController.get(this);
        controller.attach(this);
        getSystemService(NotificationManager.class).createNotificationChannel(
                new NotificationChannel(CHANNEL, "Gopurr", NotificationManager.IMPORTANCE_LOW));
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
        stopForeground(STOP_FOREGROUND_REMOVE);
        if (startId == 0) stopSelf(); else stopSelf(startId);
    }

    void refreshNotification(JSONObject state) {
        boolean chinese = getResources().getConfiguration().getLocales().get(0).getLanguage().equals("zh");
        boolean connected = state.optBoolean("connected");
        String phase = state.optString("phase");
        String description;
        if (connected) description = chinese ? "VPN 已连接" : "VPN connected";
        else if (phase.equals("disconnecting")) description = chinese ? "正在断开 VPN…" : "Disconnecting VPN…";
        else description = chinese ? "正在连接 VPN…" : "Connecting VPN…";
        PendingIntent open = PendingIntent.getActivity(this, 0,
                new Intent(this, MainActivity.class), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        PendingIntent disconnect = PendingIntent.getService(this, 1,
                new Intent(this, GopurrVpnService.class).setAction(DISCONNECT), PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Notification notice = new Notification.Builder(this, CHANNEL).setSmallIcon(R.drawable.app_icon)
                .setContentTitle("Gopurr").setContentText(description).setContentIntent(open)
                .setOngoing(true).setOnlyAlertOnce(true)
                .addAction(new Notification.Action.Builder(null, chinese ? "断开连接" : "Disconnect", disconnect).build()).build();
        if (Build.VERSION.SDK_INT >= 34) startForeground(NOTIFICATION, notice, ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED);
        else startForeground(NOTIFICATION, notice);
    }

    @Override public void onRevoke() {
        destroyed = true;
        NativeEngine.cancelPending();
        controller.revoked(this);
        super.onRevoke();
    }

    @Override public void onDestroy() {
        destroyed = true;
        if (controller != null) controller.serviceDestroyed(this);
        super.onDestroy();
    }
}
