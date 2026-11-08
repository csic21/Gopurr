package io.github.gopurr.nativeapp.test;

import android.app.Activity;
import android.content.Intent;
import android.os.Bundle;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;

import java.io.ByteArrayOutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;

/** A normal, distinct application UID: never Gopurr's excluded UID or shell. */
public final class ProbeActivity extends Activity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        probe(getIntent());
    }

    @Override public void onNewIntent(Intent request) {
        super.onNewIntent(request);
        setIntent(request);
        probe(request);
    }

    private void probe(Intent request) {
        String replyAction = request.getStringExtra("replyAction");
        String phase = request.getStringExtra("phase");
        int port = request.getIntExtra("hostPort", 0);
        String previousNetwork = request.getStringExtra("previousNetwork");
        if (replyAction == null || phase == null || port <= 0 || port > 65535) { finish(); return; }
        new Thread(() -> {
            String response = "";
            String error = "";
            String networkID = "";
            try {
                ConnectivityManager connectivity = getSystemService(ConnectivityManager.class);
                Network network = null;
                boolean expectedVPN = request.getBooleanExtra("expectedVpn", !"baseline".equals(phase));
                long deadline = android.os.SystemClock.elapsedRealtime() + 6000;
                while (android.os.SystemClock.elapsedRealtime() < deadline) {
                    Network candidate = connectivity.getActiveNetwork();
                    NetworkCapabilities capabilities = candidate == null ? null : connectivity.getNetworkCapabilities(candidate);
                    if (capabilities != null && capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN) == expectedVPN
                            && (previousNetwork == null || !previousNetwork.equals(candidate.toString()))) { network = candidate; break; }
                    android.os.SystemClock.sleep(25);
                }
                if (network == null) throw new IllegalStateException("Android did not publish the expected default network for this probe UID");
                networkID = network.toString();
                // Exercise the UID's default routing like an ordinary app.
                // Binding to an explicitly selected Network can race handover
                // and tests a different path from the default VPN policy.
                try (Socket socket = new Socket(); ByteArrayOutputStream output = new ByteArrayOutputStream()) {
                socket.connect(new InetSocketAddress("10.0.2.2", port), 4000);
                socket.setSoTimeout(4000);
                socket.getOutputStream().write(("GET /native-smoke/" + phase + " HTTP/1.0\r\nHost: 10.0.2.2\r\n\r\n").getBytes(StandardCharsets.UTF_8));
                socket.getOutputStream().flush();
                byte[] buffer = new byte[4096];
                int count;
                while ((count = socket.getInputStream().read(buffer)) != -1) output.write(buffer, 0, count);
                response = output.toString(StandardCharsets.UTF_8.name());
                }
            } catch (Exception failure) {
                error = failure.getClass().getSimpleName() + ": " + failure.getMessage();
            }
            sendBroadcast(new Intent(replyAction).setPackage("io.github.gopurr.nativeapp")
                    .putExtra("response", response).putExtra("error", error)
                    .putExtra("network", networkID)
                    .putExtra("uid", android.os.Process.myUid()));
        }, "gopurr-probe").start();
    }
}
