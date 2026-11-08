package io.github.gopurr.nativeapp;

import android.content.Context;
import android.net.VpnService;
import org.json.JSONObject;
import java.io.File;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** One in-process Go engine, shared by the Activity and the foreground service. */
public final class NativeEngine {
    public static final ExecutorService WORKER = Executors.newFixedThreadPool(3);
    public static final ExecutorService QUERIES = Executors.newSingleThreadExecutor();
    private static NativeEngine instance;
    private volatile long handle;

    static { System.loadLibrary("gopurrjni"); }

    public static synchronized NativeEngine get(Context context) throws Exception {
        if (instance == null) instance = new NativeEngine(new File(context.getFilesDir(), "gopurr"));
        return instance;
    }

    private NativeEngine(File home) throws Exception {
        JSONObject result = new JSONObject(nativeOpen(new JSONObject().put("home", home.getAbsolutePath()).toString()));
        if (result.has("error")) throw new Exception(result.getJSONObject("error").optString("message"));
        handle = result.getLong("handle");
        if (handle == 0) throw new Exception("The native engine did not open.");
    }

    public String dispatch(String request) { return nativeCall(handle, request); }
    public String startVpn(int fd, VpnService service) {
        try {
            JSONObject plan = new JSONObject(dispatch("{\"method\":\"engine.tunnelPlan\"}"));
            if (plan.has("error")) return plan.toString();
            return startVpn(fd, service, plan.getJSONObject("result").getString("token"));
        } catch (Exception error) { return SessionController.failure("invalid_plan", SessionController.message(error)); }
    }
    public String startVpn(int fd, VpnService service, String planToken) { return nativeStartVpn(handle, fd, service, planToken); }
    public String stopVpn() { return nativeStopVpn(handle); }

    public static synchronized void cancelPending() {
        if (instance != null && instance.handle != 0) nativeCancelPending(instance.handle);
    }

    public void close() {
        if (handle != 0) {
            nativeClose(handle);
            handle = 0;
        }
    }

    private static native String nativeOpen(String options);
    private static native String nativeCall(long handle, String request);
    private static native String nativeStartVpn(long handle, int fd, VpnService service, String planToken);
    private static native String nativeStopVpn(long handle);
    private static native void nativeCancelPending(long handle);
    private static native void nativeClose(long handle);
}
