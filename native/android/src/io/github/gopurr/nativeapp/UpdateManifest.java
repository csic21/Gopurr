package io.github.gopurr.nativeapp;

import org.json.JSONObject;
import java.net.URI;
import java.util.regex.Pattern;

/** The signed release feed's narrow, versioned Android update description. */
final class UpdateManifest {
    static final int SCHEMA = 1;
    private static final int MAX_NOTES_BYTES = 128 * 1024;
    private static final long MAX_APK_BYTES = 320L * 1024 * 1024;
    private static final Pattern VERSION = Pattern.compile(
            "(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?");
    private static final Pattern SHA256 = Pattern.compile("[0-9a-f]{64}");
    private static final String REPOSITORY_PATH = "/csic21/Gopurr/releases/download/";

    final String version;
    final long versionCode;
    final String notes;
    final Asset asset;

    private UpdateManifest(String version, long versionCode, String notes, Asset asset) {
        this.version = version;
        this.versionCode = versionCode;
        this.notes = notes;
        this.asset = asset;
    }

    static UpdateManifest parse(String body, String abi) throws Exception {
        JSONObject root = new JSONObject(body);
        if (root.optInt("schema", 0) != SCHEMA) {
            throw new IllegalArgumentException("The update manifest version is not supported.");
        }
        String version = root.getString("version");
        if (!VERSION.matcher(version).matches()) {
            throw new IllegalArgumentException("The update manifest has an invalid version.");
        }
        long versionCode = root.getLong("versionCode");
        if (versionCode < 1 || versionCode > 2_100_000_000L) {
            throw new IllegalArgumentException("The update manifest has an invalid version code.");
        }
        String notes = root.optString("notes", "").trim();
        if (notes.length() > MAX_NOTES_BYTES) {
            throw new IllegalArgumentException("The update notes are too large.");
        }
        JSONObject assets = root.getJSONObject("assets");
        JSONObject value = assets.optJSONObject(abi);
        if (value == null) {
            throw new IllegalArgumentException("This release does not contain an update for this device.");
        }
        String url = value.getString("url");
        URI uri = URI.create(url);
        if (!"https".equalsIgnoreCase(uri.getScheme()) || !"github.com".equalsIgnoreCase(uri.getHost())
                || uri.getUserInfo() != null || uri.getQuery() != null || uri.getFragment() != null
                || uri.getPath() == null || !uri.getPath().startsWith(REPOSITORY_PATH)) {
            throw new IllegalArgumentException("The update download address is not trusted.");
        }
        long size = value.getLong("size");
        if (size < 1 || size > MAX_APK_BYTES) {
            throw new IllegalArgumentException("The update size is not supported.");
        }
        String sha256 = value.getString("sha256").toLowerCase();
        if (!SHA256.matcher(sha256).matches()) {
            throw new IllegalArgumentException("The update checksum is invalid.");
        }
        return new UpdateManifest(version, versionCode, notes, new Asset(url, size, sha256));
    }

    static final class Asset {
        final String url;
        final long size;
        final String sha256;

        Asset(String url, long size, String sha256) {
            this.url = url;
            this.size = size;
            this.sha256 = sha256;
        }
    }
}
