package io.github.gopurr.nativeapp;

import android.net.VpnService;
import android.os.Build;
import org.json.JSONArray;
import org.json.JSONObject;

/** The engine supplies every network parameter; Android supplies the actual TUN. */
final class TunnelPlan {
    final String token;
    final JSONObject json;
    final AppScope scope;

    TunnelPlan(JSONObject object, String selfPackage) throws Exception {
        if (object.getInt("version") != 1 || !object.optBoolean("excludeSelf")) throw new IllegalArgumentException("The tunnel plan is incompatible with this host.");
        token = object.getString("token");
        if (token.isEmpty() || token.length() > 1024) throw new IllegalArgumentException("The tunnel plan token is invalid.");
        int mtu = object.getInt("mtu");
        if (mtu < 1280 || mtu > 9000) throw new IllegalArgumentException("The tunnel MTU is invalid.");
        scope = AppScope.parse(object.getJSONObject("appScope"), selfPackage);
        json = object;
    }

    VpnService.Builder configure(VpnService service) throws Exception {
        scope.verifyInstalled(service);
        VpnService.Builder builder = service.new Builder().setSession("Gopurr")
                .setMtu(json.getInt("mtu")).setBlocking(true);
        JSONArray addresses = bounded(json.getJSONArray("addresses"), "addresses");
        if (addresses.length() == 0) throw new IllegalArgumentException("The tunnel plan has no addresses.");
        for (int i = 0; i < addresses.length(); i++) {
            String[] cidr = cidr(addresses.getString(i)); builder.addAddress(cidr[0], Integer.parseInt(cidr[1]));
        }
        JSONArray routes = bounded(json.getJSONArray("routes"), "routes");
        for (int i = 0; i < routes.length(); i++) {
            String[] cidr = cidr(routes.getString(i)); builder.addRoute(cidr[0], Integer.parseInt(cidr[1]));
        }
        JSONArray dns = bounded(json.getJSONArray("dnsServers"), "DNS servers");
        for (int i = 0; i < dns.length(); i++) builder.addDnsServer(dns.getString(i));
        if (scope.mode.equals("include")) {
            for (String name : scope.packages) builder.addAllowedApplication(name);
        } else {
            builder.addDisallowedApplication(service.getPackageName());
            for (String name : scope.packages) builder.addDisallowedApplication(name);
        }
        if (Build.VERSION.SDK_INT >= 29) builder.setMetered(false);
        return builder;
    }

    private static JSONArray bounded(JSONArray array, String label) {
        if (array.length() > 64) throw new IllegalArgumentException("The tunnel plan has too many " + label + ".");
        return array;
    }

    private static String[] cidr(String value) {
        String[] parts = value.split("/", -1);
        if (parts.length != 2 || !parts[0].matches("[0-9A-Fa-f:.]+") || !parts[1].matches("[0-9]{1,3}")) throw new IllegalArgumentException("The tunnel plan contains an invalid CIDR.");
        return parts;
    }
}
