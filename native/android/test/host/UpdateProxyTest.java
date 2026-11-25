package io.github.gopurr.nativeapp;

import java.io.IOException;
import java.net.ConnectException;
import java.net.InetSocketAddress;
import java.net.Proxy;
import java.net.SocketTimeoutException;

/** Runnable on the host JVM, without Android stubs or a device. */
public final class UpdateProxyTest {
    public static void main(String[] args) {
        localProxyRequiresRunningAndValidPort();
        fallbackOnlyOnLoopbackConnectFailure();
        System.out.println("UpdateProxy: 2 regressions passed");
    }

    private static void localProxyRequiresRunningAndValidPort() {
        require(UpdateProxy.localProxy(false, 17890) == null, "Stopped engine must not produce a proxy");
        require(UpdateProxy.localProxy(true, 0) == null, "Port 0 must not produce a proxy");
        require(UpdateProxy.localProxy(true, -1) == null, "Negative port must not produce a proxy");
        require(UpdateProxy.localProxy(true, 65536) == null, "Out-of-range port must not produce a proxy");

        Proxy proxy = UpdateProxy.localProxy(true, 17890);
        require(proxy != null && proxy.type() == Proxy.Type.HTTP, "Running engine must produce an HTTP proxy");
        InetSocketAddress address = (InetSocketAddress) proxy.address();
        require(address.getAddress().isLoopbackAddress() && address.getPort() == 17890,
                "Update proxy must target 127.0.0.1:17890");
    }

    private static void fallbackOnlyOnLoopbackConnectFailure() {
        Proxy proxy = UpdateProxy.localProxy(true, 17890);
        require(UpdateProxy.shouldFallbackToDirect(proxy, new ConnectException("Connection refused")),
                "Direct ConnectException should fall back to direct");
        require(UpdateProxy.shouldFallbackToDirect(proxy, new IOException("wrapped", new ConnectException("refused"))),
                "Wrapped ConnectException should fall back to direct");
        require(!UpdateProxy.shouldFallbackToDirect(proxy, new SocketTimeoutException("timed out")),
                "Timeout through active proxy must not retry direct");
        require(!UpdateProxy.shouldFallbackToDirect(proxy, new IOException("other error")),
                "Generic IOException must not fall back to direct");
        require(!UpdateProxy.shouldFallbackToDirect(null, new ConnectException("refused")),
                "Null proxy has no fallback");
        require(!UpdateProxy.shouldFallbackToDirect(Proxy.NO_PROXY, new ConnectException("refused")),
                "Direct proxy has no fallback");
    }

    private static void require(boolean value, String message) {
        if (!value) throw new AssertionError(message);
    }
}
