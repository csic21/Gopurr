package io.github.gopurr.nativeapp;

import android.content.Context;
import android.content.pm.PackageManager;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;
import java.util.TreeSet;

/** One Builder list at a time. The host application is never captured by its VPN. */
public final class AppScope {
    public final String mode;
    public final List<String> packages;

    private AppScope(String mode, List<String> packages) {
        this.mode = mode; this.packages = Collections.unmodifiableList(packages);
    }

    public static AppScope all() { return new AppScope("all", new ArrayList<>()); }

    public static AppScope parse(JSONObject object, String selfPackage) throws Exception {
        String mode = object.optString("mode", "all");
        if (!mode.equals("all") && !mode.equals("include") && !mode.equals("exclude")) throw new IllegalArgumentException("Unknown application scope.");
        TreeSet<String> packages = new TreeSet<>();
        JSONArray input = object.optJSONArray("packages");
        if (input != null && input.length() > 256) throw new IllegalArgumentException("Select no more than 256 applications.");
        if (input != null) for (int i = 0; i < input.length(); i++) {
            String name = input.getString(i).trim();
            if (name.length() > 256 || !name.matches("[A-Za-z][A-Za-z0-9_]*(?:\\.[A-Za-z][A-Za-z0-9_]*)*")) throw new IllegalArgumentException("An application package name is invalid.");
            if (mode.equals("include") && name.equals(selfPackage)) throw new IllegalArgumentException("Gopurr cannot include itself in its VPN.");
            if (!name.equals(selfPackage)) packages.add(name);
        }
        if (mode.equals("all")) packages.clear();
        if (mode.equals("include") && packages.isEmpty()) throw new IllegalArgumentException("Select at least one application to include.");
        return new AppScope(mode, new ArrayList<>(packages));
    }

    public void verifyInstalled(Context context) throws Exception {
        PackageManager manager = context.getPackageManager();
        for (String name : packages) {
            try { manager.getApplicationInfo(name, 0); }
            catch (PackageManager.NameNotFoundException error) { throw new IllegalArgumentException("A selected application is no longer installed: " + name); }
        }
    }

    public JSONObject json() {
        JSONObject object = engineJson();
        try { object.put("excludeSelf", true); }
        catch (Exception ignored) { }
        return object;
    }

    JSONObject engineJson() {
        JSONObject object = new JSONObject();
        try { object.put("mode", mode).put("packages", new JSONArray(packages)); }
        catch (Exception ignored) { }
        return object;
    }
}
