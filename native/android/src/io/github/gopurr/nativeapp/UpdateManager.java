package io.github.gopurr.nativeapp;

import android.app.PendingIntent;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ApplicationInfo;
import android.content.pm.PackageInfo;
import android.content.pm.PackageInstaller;
import android.content.pm.PackageManager;
import android.content.pm.Signature;
import android.net.Uri;
import android.os.Build;
import android.os.Handler;
import android.os.Looper;
import android.provider.Settings;
import org.json.JSONObject;
import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.ByteArrayOutputStream;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.URL;
import java.security.MessageDigest;
import java.util.Set;
import java.util.TreeSet;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import javax.net.ssl.HttpsURLConnection;

/**
 * Downloads a signed Android update and hands it to the system installer.
 *
 * The release feed and APK are both fetched over HTTPS, but the security
 * boundary is the APK's installed signing certificate: Android only accepts a
 * package update signed by the same certificate as the current package.
 */
final class UpdateManager {
    interface Listener {
        void onUpdate(JSONObject snapshot);
    }

    private static final String FEED = "https://github.com/csic21/Gopurr/releases/latest/download/update-android.json";
    private static final int MAX_MANIFEST_BYTES = 64 * 1024;
    private static final long MAX_APK_BYTES = 320L * 1024 * 1024;
    private static final int MAX_REDIRECTS = 5;
    private static final ExecutorService WORKER = Executors.newSingleThreadExecutor();
    private static UpdateManager instance;

    private final Context context;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final PackageManager packages;
    private final String packageName;
    private final String currentVersion;
    private final long currentVersionCode;
    private final String abi;
    private final boolean supported;
    private Listener listener;
    private String phase;
    private String version = "";
    private String notes = "";
    private String error = "";
    private String detail = "";
    private long availableVersionCode;
    private long downloaded;
    private long total;
    private UpdateManifest.Asset asset;
    private File apk;
    private int installSession;
    private boolean autoChecked;
    private boolean canInstall;
    private volatile boolean cancelled;
    private volatile HttpsURLConnection connection;

    private UpdateManager(Context context) {
        this.context = context;
        packages = context.getPackageManager();
        packageName = context.getPackageName();
        String configuredVersion;
        long configuredCode;
        try {
            PackageInfo info = packages.getPackageInfo(packageName, 0);
            configuredVersion = info.versionName == null ? "dev" : info.versionName;
            configuredCode = info.getLongVersionCode();
        } catch (PackageManager.NameNotFoundException impossible) {
            configuredVersion = "dev";
            configuredCode = 0;
        }
        currentVersion = configuredVersion;
        currentVersionCode = configuredCode;
        abi = selectAbi();
        supported = (context.getApplicationInfo().flags & ApplicationInfo.FLAG_DEBUGGABLE) == 0 && abi != null;
        canInstall = installAllowed();
        phase = supported ? "idle" : "unsupported";
    }

    static synchronized UpdateManager get(Context context) {
        if (instance == null) instance = new UpdateManager(context.getApplicationContext());
        return instance;
    }

    void setListener(Listener value) {
        synchronized (this) { listener = value; }
        if (value != null) value.onUpdate(snapshot());
    }

    void clearListener(Listener value) {
        synchronized (this) {
            if (listener == value) listener = null;
        }
    }

    void autoCheck() {
        synchronized (this) {
            if (autoChecked || !supported) return;
            autoChecked = true;
        }
        check();
    }

    void refreshPermission() {
        boolean next = installAllowed();
        synchronized (this) {
            if (next == canInstall) return;
            canInstall = next;
        }
        notifyChange();
    }

    JSONObject check() {
        synchronized (this) {
            if (!supported || busy()) return snapshot();
            resetAvailable();
            phase = "checking";
            cancelled = false;
        }
        notifyChange();
        WORKER.execute(this::checkWorker);
        return snapshot();
    }

    JSONObject download() {
        synchronized (this) {
            if (!supported || !phase.equals("available") || asset == null) return snapshot();
            phase = "downloading";
            error = "";
            detail = "";
            downloaded = 0;
            total = asset.size;
            cancelled = false;
        }
        notifyChange();
        WORKER.execute(this::downloadWorker);
        return snapshot();
    }

    JSONObject cancel() {
        synchronized (this) {
            if (!phase.equals("checking") && !phase.equals("downloading")) return snapshot();
            cancelled = true;
        }
        HttpsURLConnection active = connection;
        if (active != null) active.disconnect();
        notifyChange();
        return snapshot();
    }

    JSONObject install() {
        boolean openSettings = false;
        synchronized (this) {
            if (!supported || !phase.equals("ready") || apk == null) return snapshot();
            canInstall = installAllowed();
            if (!canInstall) {
                error = "请允许 Gopurr 安装未知应用，然后重试。";
                detail = "";
                openSettings = true;
            } else {
                phase = "installing";
                error = "";
                detail = "正在准备系统安装…";
                installSession = 0;
            }
        }
        notifyChange();
        if (openSettings) {
            main.post(this::openInstallSettings);
            return snapshot();
        }
        WORKER.execute(this::installWorker);
        return snapshot();
    }

    void installResult(int session, int status, String message, Intent userAction) {
        synchronized (this) {
            if (!phase.equals("installing")) return;
            if (installSession != 0 && session != installSession) return;
        }
        if (status == PackageInstaller.STATUS_PENDING_USER_ACTION) {
            synchronized (this) {
                phase = "installing";
                detail = "请在系统安装程序中确认。";
                error = "";
            }
            notifyChange();
            if (userAction != null) {
                final Intent action = userAction;
                action.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
                main.post(() -> {
                    try { context.startActivity(action); }
                    catch (RuntimeException failure) {
                        synchronized (UpdateManager.this) {
                            phase = "ready";
                            error = "无法打开系统安装程序。";
                            detail = "";
                        }
                        notifyChange();
                    }
                });
            }
            return;
        }
        synchronized (this) {
            installSession = 0;
            detail = "";
            if (status == PackageInstaller.STATUS_SUCCESS) {
                phase = "installed";
                error = "";
                deleteDownloaded();
            } else {
                phase = "ready";
                error = installationError(status, message);
            }
        }
        notifyChange();
    }

    JSONObject snapshot() {
        JSONObject state = new JSONObject();
        try {
            state.put("supported", supported);
            state.put("phase", phase);
            state.put("currentVersion", currentVersion);
            state.put("currentVersionCode", currentVersionCode);
            state.put("version", version);
            state.put("notes", notes);
            state.put("downloaded", downloaded);
            state.put("total", total);
            state.put("error", error);
            state.put("detail", detail);
            state.put("canInstall", canInstall);
        } catch (Exception ignored) { }
        return state;
    }

    private boolean busy() {
        return phase.equals("checking") || phase.equals("downloading") || phase.equals("installing");
    }

    private void resetAvailable() {
        version = "";
        notes = "";
        error = "";
        detail = "";
        downloaded = 0;
        total = 0;
        availableVersionCode = 0;
        asset = null;
        apk = null;
    }

    private void checkWorker() {
        try {
            UpdateManifest manifest = UpdateManifest.parse(readText(FEED, MAX_MANIFEST_BYTES), abi);
            synchronized (this) {
                if (cancelled) {
                    cancelled = false;
                    phase = "idle";
                    return;
                }
                version = manifest.version;
                notes = manifest.notes;
                detail = "";
                error = "";
                if (manifest.versionCode <= currentVersionCode) {
                    phase = "current";
                    availableVersionCode = 0;
                    asset = null;
                    total = 0;
                } else {
                    phase = "available";
                    availableVersionCode = manifest.versionCode;
                    asset = manifest.asset;
                    total = manifest.asset.size;
                    downloaded = 0;
                }
            }
        } catch (Exception failure) {
            synchronized (this) {
                if (cancelled) {
                    cancelled = false;
                    phase = "idle";
                    error = "";
                } else {
                    phase = "failed";
                    error = message(failure);
                }
            }
        }
        notifyChange();
    }

    private void downloadWorker() {
        File part = null;
        UpdateManifest.Asset target;
        long targetCode;
        String targetVersion;
        synchronized (this) {
            target = asset;
            targetCode = availableVersionCode;
            targetVersion = version;
        }
        try {
            if (target == null || targetCode < 1 || !abi.equals(selectAbi())) {
                throw new IllegalStateException("The update target is no longer available.");
            }
            File directory = new File(context.getCacheDir(), "updates");
            if (!directory.isDirectory() && !directory.mkdirs()) {
                throw new IOException("Could not create the update directory.");
            }
            clearFiles(directory);
            File destination = new File(directory, "gopurr-" + targetCode + "-" + abi + ".apk");
            part = new File(directory, destination.getName() + ".part");
            HttpsURLConnection active = open(target.url);
            connection = active;
            try {
                int code = active.getResponseCode();
                if (code != HttpsURLConnection.HTTP_OK) {
                    throw new IOException("The update server returned HTTP " + code + ".");
                }
                long length = active.getContentLengthLong();
                if (length >= 0 && length != target.size) {
                    throw new IOException("The update size does not match the release manifest.");
                }
                MessageDigest digest = MessageDigest.getInstance("SHA-256");
                long received = 0;
                long lastReported = 0;
                try (InputStream input = new BufferedInputStream(active.getInputStream());
                     OutputStream output = new BufferedOutputStream(new FileOutputStream(part))) {
                    byte[] buffer = new byte[64 * 1024];
                    int count;
                    while ((count = input.read(buffer)) != -1) {
                        if (cancelled) throw new IOException("cancelled");
                        received += count;
                        if (received > target.size) throw new IOException("The update is larger than the release manifest.");
                        output.write(buffer, 0, count);
                        digest.update(buffer, 0, count);
                        if (received - lastReported >= 512 * 1024) {
                            lastReported = received;
                            synchronized (this) { downloaded = received; }
                            notifyChange();
                        }
                    }
                }
                if (cancelled) throw new IOException("cancelled");
                if (part.length() != target.size) throw new IOException("The downloaded update is incomplete.");
                String actual = hex(digest.digest());
                if (!MessageDigest.isEqual(actual.getBytes(java.nio.charset.StandardCharsets.US_ASCII),
                        target.sha256.getBytes(java.nio.charset.StandardCharsets.US_ASCII))) {
                    throw new IOException("The update checksum did not match the release manifest.");
                }
                verifyApk(part, targetCode, targetVersion);
                if (destination.exists() && !destination.delete()) {
                    throw new IOException("Could not replace the previous update.");
                }
                if (!part.renameTo(destination)) throw new IOException("Could not finalize the downloaded update.");
                part = null;
                synchronized (this) {
                    apk = destination;
                    downloaded = target.size;
                    total = target.size;
                    phase = "ready";
                    error = "";
                    detail = canInstall ? "" : "请允许 Gopurr 安装未知应用，然后重试。";
                    cancelled = false;
                }
            } finally {
                connection = null;
                active.disconnect();
            }
        } catch (Exception failure) {
            if (part != null) part.delete();
            synchronized (this) {
                if (cancelled) {
                    phase = "available";
                    downloaded = 0;
                    error = "";
                    detail = "";
                } else {
                    phase = "available";
                    error = message(failure);
                    detail = "";
                }
                cancelled = false;
            }
        }
        notifyChange();
    }

    private void installWorker() {
        try {
            File source;
            long expectedCode;
            String expectedVersion;
            synchronized (this) {
                source = apk;
                expectedCode = availableVersionCode;
                expectedVersion = version;
            }
            if (source == null || !source.isFile()) throw new IOException("The downloaded update is missing.");
            verifyApk(source, expectedCode, expectedVersion);
            PackageInstaller installer = packages.getPackageInstaller();
            PackageInstaller.SessionParams params = new PackageInstaller.SessionParams(
                    PackageInstaller.SessionParams.MODE_FULL_INSTALL);
            params.setSize(source.length());
            int session = installer.createSession(params);
            synchronized (this) { installSession = session; }
            Intent result = new Intent(context, UpdateInstallReceiver.class)
                    .setAction(UpdateInstallReceiver.ACTION_STATUS)
                    .putExtra(UpdateInstallReceiver.EXTRA_SESSION, session);
            int flags = PendingIntent.FLAG_UPDATE_CURRENT;
            if (Build.VERSION.SDK_INT >= 31) flags |= PendingIntent.FLAG_MUTABLE;
            PendingIntent pending = PendingIntent.getBroadcast(context, session, result, flags);
            PackageInstaller.Session opened = installer.openSession(session);
            try {
                try (InputStream input = new FileInputStream(source);
                     OutputStream output = opened.openWrite("base.apk", 0, source.length())) {
                    byte[] buffer = new byte[64 * 1024];
                    int count;
                    while ((count = input.read(buffer)) != -1) output.write(buffer, 0, count);
                    opened.fsync(output);
                }
                opened.commit(pending.getIntentSender());
            } finally {
                opened.close();
            }
            synchronized (this) {
                phase = "installing";
                detail = "正在等待系统安装程序…";
                error = "";
            }
        } catch (Exception failure) {
            synchronized (this) {
                phase = "ready";
                error = message(failure);
                detail = "";
                installSession = 0;
            }
        }
        notifyChange();
    }

    private void openInstallSettings() {
        Intent intent = new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,
                Uri.parse("package:" + packageName));
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
        try {
            context.startActivity(intent);
        } catch (RuntimeException failure) {
            synchronized (this) { error = "无法打开安装权限设置。"; }
            notifyChange();
        }
    }

    private String readText(String address, int limit) throws Exception {
        HttpsURLConnection active = open(address);
        connection = active;
        try {
            int code = active.getResponseCode();
            if (code != HttpsURLConnection.HTTP_OK) {
                if (code == HttpsURLConnection.HTTP_NOT_FOUND) {
                    throw new IOException("The latest release does not provide an Android update manifest yet.");
                }
                throw new IOException("The update server returned HTTP " + code + ".");
            }
            long length = active.getContentLengthLong();
            if (length > limit) throw new IOException("The update manifest is too large.");
            ByteArrayOutputStream output = new ByteArrayOutputStream();
            try (InputStream input = active.getInputStream()) {
                byte[] buffer = new byte[8 * 1024];
                int count;
                while ((count = input.read(buffer)) != -1) {
                    if (output.size() + count > limit) throw new IOException("The update manifest is too large.");
                    output.write(buffer, 0, count);
                }
            }
            return output.toString("UTF-8");
        } finally {
            connection = null;
            active.disconnect();
        }
    }

    private HttpsURLConnection open(String address) throws IOException {
        URL url = new URL(address);
        for (int attempt = 0; attempt <= MAX_REDIRECTS; attempt++) {
            if (!"https".equalsIgnoreCase(url.getProtocol())) {
                throw new IOException("Update downloads must use HTTPS.");
            }
            HttpsURLConnection active = (HttpsURLConnection) url.openConnection();
            active.setInstanceFollowRedirects(false);
            active.setConnectTimeout(15_000);
            active.setReadTimeout(30_000);
            active.setUseCaches(false);
            active.setRequestProperty("User-Agent", "Gopurr/" + currentVersion + " (Android)");
            active.setRequestProperty("Accept", "application/json, application/octet-stream");
            int code = active.getResponseCode();
            if (code >= 300 && code < 400) {
                String location = active.getHeaderField("Location");
                active.disconnect();
                if (location == null) throw new IOException("The update server returned an invalid redirect.");
                url = new URL(url, location);
                continue;
            }
            return active;
        }
        throw new IOException("The update server redirected too many times.");
    }

    private void verifyApk(File file, long expectedCode, String expectedVersion) throws Exception {
        PackageInfo archive = packages.getPackageArchiveInfo(file.getAbsolutePath(),
                PackageManager.GET_SIGNING_CERTIFICATES);
        if (archive == null) throw new IOException("The downloaded APK could not be read.");
        if (!packageName.equals(archive.packageName)) {
            throw new IOException("The downloaded APK belongs to another application.");
        }
        if (archive.getLongVersionCode() != expectedCode) {
            throw new IOException("The downloaded APK version code does not match the release manifest.");
        }
        if (expectedVersion == null || !expectedVersion.equals(archive.versionName)) {
            throw new IOException("The downloaded APK version does not match the release manifest.");
        }
        PackageInfo installed = packages.getPackageInfo(packageName, PackageManager.GET_SIGNING_CERTIFICATES);
        if (!signerDigests(installed).equals(signerDigests(archive))) {
            throw new IOException("The downloaded APK was not signed by Gopurr's installed signing certificate.");
        }
    }

    private static Set<String> signerDigests(PackageInfo info) throws Exception {
        if (info.signingInfo == null) throw new IOException("The APK has no signing information.");
        Signature[] signatures = info.signingInfo.getApkContentsSigners();
        if (signatures == null || signatures.length == 0) throw new IOException("The APK has no signer.");
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        TreeSet<String> values = new TreeSet<>();
        for (Signature signature : signatures) values.add(hex(digest.digest(signature.toByteArray())));
        return values;
    }

    private boolean installAllowed() {
        return Build.VERSION.SDK_INT < 26 || packages.canRequestPackageInstalls();
    }

    private static String selectAbi() {
        for (String value : Build.SUPPORTED_ABIS) {
            if (value.equals("arm64-v8a") || value.equals("x86_64")) return value;
        }
        return null;
    }

    private static void clearFiles(File directory) {
        File[] entries = directory.listFiles();
        if (entries == null) return;
        for (File entry : entries) entry.delete();
    }

    private void deleteDownloaded() {
        if (apk != null) apk.delete();
        apk = null;
    }

    private void notifyChange() {
        Listener current;
        synchronized (this) { current = listener; }
        if (current == null) return;
        JSONObject state = snapshot();
        main.post(() -> {
            Listener next;
            synchronized (this) { next = listener; }
            if (next != null) next.onUpdate(state);
        });
    }

    private static String message(Exception failure) {
        String value = failure.getMessage();
        return value == null || value.trim().isEmpty() ? "The update could not be completed." : value;
    }

    private static String installationError(int status, String message) {
        if (message != null && !message.trim().isEmpty()) return message;
        switch (status) {
            case PackageInstaller.STATUS_FAILURE_ABORTED: return "系统取消了安装。";
            case PackageInstaller.STATUS_FAILURE_BLOCKED: return "系统阻止了安装，请检查安装权限。";
            case PackageInstaller.STATUS_FAILURE_CONFLICT: return "安装包与已安装应用冲突。";
            case PackageInstaller.STATUS_FAILURE_INCOMPATIBLE: return "安装包与当前设备不兼容。";
            case PackageInstaller.STATUS_FAILURE_INVALID: return "安装包无效或签名不匹配。";
            case PackageInstaller.STATUS_FAILURE_STORAGE: return "设备存储空间不足。";
            default: return "系统未能安装更新。";
        }
    }

    private static String hex(byte[] bytes) {
        char[] digits = "0123456789abcdef".toCharArray();
        char[] value = new char[bytes.length * 2];
        for (int i = 0; i < bytes.length; i++) {
            int byteValue = bytes[i] & 0xff;
            value[i * 2] = digits[byteValue >>> 4];
            value[i * 2 + 1] = digits[byteValue & 0x0f];
        }
        return new String(value);
    }
}
