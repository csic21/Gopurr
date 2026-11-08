package io.github.gopurr.nativeapp;

import android.Manifest;
import android.app.Activity;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.content.ContentResolver;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.database.Cursor;
import android.net.Uri;
import android.os.Build;
import android.provider.OpenableColumns;
import android.provider.Settings;
import android.view.View;
import android.view.Window;
import android.view.WindowInsetsController;
import com.longbridge.nsh.NshHostReply;
import org.json.JSONObject;
import java.io.ByteArrayOutputStream;
import java.io.InputStream;
import java.nio.ByteBuffer;
import java.nio.charset.CodingErrorAction;
import java.nio.charset.StandardCharsets;

/** System pickers and optional notification permission belong to the visible Activity. */
final class NativeActions {
    static final int PICK_FILE = 20_000;
    static final int NOTIFICATIONS = 20_001;
    private static final int MAX_PROFILE_BYTES = 8 * 1024 * 1024;
    private final Activity activity;
    private NshHostReply fileReply;
    private NshHostReply notificationReply;
    private String appearance;

    NativeActions(Activity activity) { this.activity = activity; }

    JSONObject appearance(JSONObject value) throws Exception {
        String scheme = value.optString("scheme");
        if (!scheme.equals("light") && !scheme.equals("dark")) throw new IllegalArgumentException("Appearance must be light or dark.");
        appearance = scheme;
        activity.getWindow().getDecorView().post(this::applyAppearance);
        return new JSONObject().put("scheme", scheme);
    }

    void applyAppearance() {
        if (appearance == null) return;
        Window window = activity.getWindow();
        boolean light = appearance.equals("light");
        if (Build.VERSION.SDK_INT >= 30 && window.getInsetsController() != null) {
            int mask = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS | WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS;
            window.getInsetsController().setSystemBarsAppearance(light ? mask : 0, mask);
        } else {
            int mask = View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR | View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;
            int flags = window.getDecorView().getSystemUiVisibility();
            window.getDecorView().setSystemUiVisibility(light ? flags | mask : flags & ~mask);
        }
        if (Build.VERSION.SDK_INT >= 29) {
            window.setStatusBarContrastEnforced(false);
            window.setNavigationBarContrastEnforced(false);
        }
    }

    void pickFile(NshHostReply reply) {
        if (fileReply != null) { reply.reject("A file chooser is already open."); return; }
        Intent picker = new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE)
                .setType("*/*").putExtra(Intent.EXTRA_MIME_TYPES,
                        new String[]{"text/*", "application/yaml", "application/x-yaml", "application/octet-stream"})
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        fileReply = reply;
        try { activity.startActivityForResult(picker, PICK_FILE); }
        catch (RuntimeException error) { fileReply = null; reply.reject(SessionController.message(error)); }
    }

    boolean activityResult(int request, int result, Intent data) {
        if (request != PICK_FILE) return false;
        NshHostReply reply = fileReply; fileReply = null;
        if (reply == null) return true;
        if (result != Activity.RESULT_OK || data == null || data.getData() == null) {
            try { reply.resolve(new JSONObject().put("cancelled", true)); }
            catch (Exception ignored) { reply.resolve(null); }
            return true;
        }
        Uri uri = data.getData();
        ContentResolver resolver = activity.getApplicationContext().getContentResolver();
        NativeEngine.WORKER.execute(() -> {
            try { reply.resolve(read(resolver, uri)); }
            catch (Exception error) { reply.reject(SessionController.message(error)); }
        });
        return true;
    }

    private static JSONObject read(ContentResolver resolver, Uri uri) throws Exception {
        String name = "configuration.yaml";
        try (Cursor cursor = resolver.query(uri, new String[]{OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE}, null, null, null)) {
            if (cursor != null && cursor.moveToFirst()) {
                int nameColumn = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME);
                if (nameColumn >= 0 && !cursor.isNull(nameColumn)) name = cursor.getString(nameColumn);
                int sizeColumn = cursor.getColumnIndex(OpenableColumns.SIZE);
                if (sizeColumn >= 0 && !cursor.isNull(sizeColumn) && cursor.getLong(sizeColumn) > MAX_PROFILE_BYTES) throw new IllegalArgumentException("The selected configuration exceeds 8 MiB.");
            }
        }
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        try (InputStream input = resolver.openInputStream(uri)) {
            if (input == null) throw new IllegalArgumentException("The selected file could not be opened.");
            byte[] buffer = new byte[16 * 1024]; int count;
            while ((count = input.read(buffer)) != -1) {
                if (output.size() + count > MAX_PROFILE_BYTES) throw new IllegalArgumentException("The selected configuration exceeds 8 MiB.");
                output.write(buffer, 0, count);
            }
        }
        String text = StandardCharsets.UTF_8.newDecoder().onMalformedInput(CodingErrorAction.REPORT)
                .onUnmappableCharacter(CodingErrorAction.REPORT).decode(ByteBuffer.wrap(output.toByteArray())).toString();
        if (text.startsWith("\uFEFF")) text = text.substring(1);
        if (text.isEmpty()) throw new IllegalArgumentException("The selected file is empty.");
        return new JSONObject().put("name", name).put("content", text).put("cancelled", false);
    }

    JSONObject notifications() {
        boolean granted = Build.VERSION.SDK_INT < 33 || activity.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED;
        NotificationManager manager = activity.getSystemService(NotificationManager.class);
        NotificationChannel channel = manager.getNotificationChannel(GopurrVpnService.CHANNEL);
        boolean enabled = granted && manager.areNotificationsEnabled()
                && (channel == null || channel.getImportance() != NotificationManager.IMPORTANCE_NONE);
        boolean requested = activity.getSharedPreferences("android-session", Activity.MODE_PRIVATE).getBoolean("notificationRequested", false);
        boolean canRequest = Build.VERSION.SDK_INT >= 33 && !granted && (!requested || activity.shouldShowRequestPermissionRationale(Manifest.permission.POST_NOTIFICATIONS));
        JSONObject state = new JSONObject();
        try { state.put("granted", granted).put("enabled", enabled).put("canRequest", canRequest)
                .put("traffic", activity.getSharedPreferences("android-session", Activity.MODE_PRIVATE).getBoolean("notificationTraffic", true)); }
        catch (Exception ignored) { }
        return state;
    }

    JSONObject configureNotifications(JSONObject value) {
        Object traffic = value.opt("traffic");
        if (!(traffic instanceof Boolean)) throw new IllegalArgumentException("Notification traffic must be true or false.");
        activity.getSharedPreferences("android-session", Activity.MODE_PRIVATE).edit().putBoolean("notificationTraffic", (Boolean) traffic).apply();
        refreshNotifications();
        return notifications();
    }

    JSONObject openNotificationSettings() {
        NotificationChannel channel = activity.getSystemService(NotificationManager.class).getNotificationChannel(GopurrVpnService.CHANNEL);
        Intent intent = new Intent(channel == null ? Settings.ACTION_APP_NOTIFICATION_SETTINGS : Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS)
                .putExtra(Settings.EXTRA_APP_PACKAGE, activity.getPackageName());
        if (channel != null) intent.putExtra(Settings.EXTRA_CHANNEL_ID, GopurrVpnService.CHANNEL);
        activity.startActivity(intent);
        return notifications();
    }

    void refreshNotifications() {
        activity.sendBroadcast(new Intent(GopurrVpnService.NOTIFICATION_CHANGED).setPackage(activity.getPackageName()));
    }

    void requestNotifications(NshHostReply reply) {
        JSONObject state = notifications();
        if (state.optBoolean("granted") || !state.optBoolean("canRequest")) { reply.resolve(state); return; }
        if (notificationReply != null) { reply.reject("A notification permission request is already open."); return; }
        notificationReply = reply;
        activity.getSharedPreferences("android-session", Activity.MODE_PRIVATE).edit().putBoolean("notificationRequested", true).apply();
        activity.requestPermissions(new String[]{Manifest.permission.POST_NOTIFICATIONS}, NOTIFICATIONS);
    }

    void permissionsResult(int request) {
        if (request != NOTIFICATIONS || notificationReply == null) return;
        NshHostReply reply = notificationReply; notificationReply = null; reply.resolve(notifications());
        refreshNotifications();
    }

    void close() {
        if (fileReply != null) { fileReply.reject("The file chooser was closed."); fileReply = null; }
        if (notificationReply != null) { notificationReply.reject("The notification prompt was closed."); notificationReply = null; }
    }
}
