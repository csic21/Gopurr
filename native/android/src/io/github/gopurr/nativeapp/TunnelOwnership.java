package io.github.gopurr.nativeapp;

/** Used on the serial control executor: stale work can only take its own TUN. */
final class TunnelOwnership<T> {
    private T owner;
    private long generation;
    void assign(T owner, long generation) { this.owner = owner; this.generation = generation; }
    T takeCurrent() { T result = owner; owner = null; generation = 0; return result; }
    T takeIf(T expected, long expectedGeneration) {
        if (owner != expected || (expectedGeneration != 0 && generation != expectedGeneration)) return null;
        return takeCurrent();
    }
    T takeThrough(T expected, long lastGeneration) {
        if (owner != expected || generation > lastGeneration) return null;
        return takeCurrent();
    }
}
