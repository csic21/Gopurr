package io.github.gopurr.nativeapp;

import java.io.IOException;
import java.net.ConnectException;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Proxy;

/** Resolves the running local mihomo HTTP proxy for in-app Android update requests. */
final class UpdateProxy {
    private UpdateProxy() { }

    static Proxy localProxy(boolean running, int mixedPort) {
        if (!running || mixedPort < 1 || mixedPort > 65535) return null;
        return new Proxy(Proxy.Type.HTTP, new InetSocketAddress(InetAddress.getLoopbackAddress(), mixedPort));
    }

    static boolean shouldFallbackToDirect(Proxy proxy, IOException failure) {
        if (proxy == null || proxy.type() == Proxy.Type.DIRECT || failure == null) return false;
        Throwable current = failure;
        for (int depth = 0; depth < 8 && current != null; depth++) {
            if (current instanceof ConnectException) return true;
            Throwable next = current.getCause();
            if (next == current) break;
            current = next;
        }
        return false;
    }
}
