package io.github.gopurr.nativeapp;

public final class TrafficTextTest {
    public static void main(String[] args) {
        require(TrafficText.bytes(-1).equals("0 B"));
        require(TrafficText.bytes(1023).equals("1023 B"));
        require(TrafficText.bytes(1024).equals("1.0 KB"));
        require(TrafficText.bytes(1536).equals("1.5 KB"));
        require(TrafficText.bytes(1024L*1024*1024*1024).equals("1.0 TB"));
        require(TrafficText.rates(true, 1024, 0).equals("下载 1.0 KB/s   上传 0 B/s"));
        require(TrafficText.totals(false, 2048, 1024).equals("Downloaded 2.0 KB   Uploaded 1.0 KB"));
        System.out.println("TrafficText: notification formatting passed");
    }
    private static void require(boolean value) { if (!value) throw new AssertionError("Invalid notification text"); }
}
