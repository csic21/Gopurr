# Gopurr engine API

The shared Go engine runs without mygo, a desktop window, a privileged helper,
or OS proxy changes. Native hosts call it directly or through
`cmd/gopurr-bridge`'s C ABI. Android's `VpnService` supplies the VPN descriptor
and socket protector separately; JSON requests cannot attach arbitrary fds.

```go
instance, err := engine.Open(engine.Options{Home: absolutePrivateDirectory})
if err != nil {
    return err
}
defer instance.Close()
response := instance.Dispatch(ctx, []byte(`{"method":"engine.status"}`))
```

Use a private application directory, separate from existing desktop data. No
profile, desktop setting, update credential, or proxy service is discovered
implicitly. A zero `MixedPort` selects an available loopback TCP/UDP port;
both listeners close on stop. Hosts must authenticate any transport exposing
the dispatch function; it is not an unauthenticated network server.

## Protocol version 1

Requests are `{id?, method, params?}`. Responses preserve `id` and contain
either `result` or `error: {code, message}`. Fields use lower camel case.

| Method | Parameters | Result |
| --- | --- | --- |
| `engine.status` | none | Status, capabilities and real traffic counters |
| `engine.traffic` | none | `{up, down, upTotal, downTotal, at}`; lightweight counters without connection enumeration or memory sampling |
| `engine.start` | none | Start selected profile; idempotent if already running |
| `engine.stop` | none | Disconnect and release owned listeners |
| `engine.mode` | `{mode: "rule" \| "global" \| "direct"}` | Status |
| `engine.tunnelPlan` | `{appScope?: {mode, packages}}` | Versioned TUN plan and opaque token |
| `profiles.list` | none | `{active, profiles}` |
| `profiles.import` | `{name?, url}` or `{name?, yaml}` | Profile summary |
| `profiles.activate` | `{id}` | Load selected profile and start local core |
| `profiles.editor` | `{id}` | `{id, name, url, kind}` for an explicit source editor |
| `profiles.update` | `{id, name?, url?}` | Saved source metadata; does not download |
| `profiles.refresh` | `{id}` | Download and validate, then atomically save |
| `profiles.delete` | `{id}` | Delete a stopped or inactive profile |
| `proxies.groups` | none | `{groups, preview?, incomplete?}` in configuration order; stopped engines return saved-node previews |
| `proxies.select` | `{group, member}` | `{ok: true}` |
| `proxies.delay` | `{name, url?}` | `{name, delay}` in milliseconds |
| `logs.list` | none | `{logs}`; latest 300 entries |

Imports are bounded to 16 MiB. HTTP downloads and delay tests observe their
context. Summaries expose source hostnames, never source URLs, subscription
tokens, passwords or YAML. Local documents retain a portable cached source,
not the native file picker's temporary path.

Disconnected group reads compose the saved profile and inspect local YAML only;
they never instantiate adapters, start the core, refresh providers or run health
checks. They include inline payloads and regular provider cache files inside the
kernel data directory, strip credentials and omit latency. `preview=true` marks
this static view; `incomplete=true` identifies missing/unreadable caches or
dynamic provider-name expressions that require the live core. Manual selectors
show a still-valid saved choice, otherwise their first visible member; automatic
groups do not claim a current exit. Selecting and testing still require a running
core. Stopped mobile pages do not periodically reparse this preview.

New mutations return `{profile?, saved, applied, reconnectRequired, status}`.
The original `profiles.import` direct summary response stays compatible. Editing
a source URL preserves the existing cached configuration until a successful
refresh. Renaming never marks a connection as requiring a restart. Refreshing
an active source saves the new valid bytes without replacing the live VPN:
`saved=true`, `applied=false`, `reconnectRequired=true`. Disconnect/reconnect
accepts the staged content and clears the flag. A running active profile cannot
be deleted. Only the explicit editor returns a credential-bearing source URL;
list/status/mutation summaries remain redacted.

Import/refresh and activation reuse `internal/workflow` and profile Store's
context-aware checked commits. Status includes a profile `busy` flag and
`operation: {method, profileId?, phase}`. Downloads and delay tests run outside
the core state mutex: status stays readable and disconnect cancels outstanding
network calls. Late delay results cannot enter a newer runtime's cache.

Runtime assembly reuses Gopurr's profile and YAML composition code, then owns
all management endpoints, listeners, DNS listening, TUN, process discovery,
NTP and OS routing. Desktop/local mode offers only loopback HTTP/SOCKS. Android
VPN mode configures the system-provided TUN and protects outbound sockets.
Status distinguishes the core running from a VPN being attached.

## Ownership and lifecycle

Mihomo has process-wide singleton state. Open only one engine at a time. A host
can reopen the same directory after Close, but must restart its process to
choose a different directory. Changes have one profile writer; host callbacks
run on worker threads. Close cancels outstanding operations and unsubscribes
the engine's log consumer.

Profile activation validates before touching a working core. Applying a
different configuration explicitly disconnects old traffic before loading;
this is a reconnect, not a seamless reload. A failure restores the previous
working runtime, routing mode and manual node selections. Failed imports do
not change the existing cache or selection. While Android VPN is attached,
disconnect before changing profiles to avoid reusing an owned TUN descriptor.

Successful manual group choices are persisted per profile, alongside the
engine's routing mode, in private schema-1 `mobile-runtime.json`. Engine
Close/Open, host-process restarts, disconnect/reconnect, switching back to a
profile and attaching an Android VPN restore those choices. Opening an engine
never starts a connection. A refreshed configuration that removes a chosen group
or member drops that preference and uses its configuration default. Deleting a
profile clears its choices; names shared by different subscriptions do not
share preferences. Deleted profile IDs are also filtered when loading.

Preference writes use atomic replacement and 0600 permissions. A failed mode
save leaves routing unchanged; a failed node save restores the previous live
choice and does not commit the draft. Pruning follows an already accepted
configuration/deletion: a cleanup write failure leaves the successful operation
in place, reports a warning through Status.error and retries on the next write.
Homes from older builds without a preference file keep defaults. Corrupt,
incompatible or linked preference files fail Open with `preferences_failed`
without overwriting their contents; hosts must show that error rather than
silently resetting routing. This file stores no source URLs or node credentials.

## Android tunnel plan

`engine.tunnelPlan` returns `version: 1`, `token`, `mtu`, CIDR `addresses`, CIDR
`routes`, `dnsServers`, `ipv6`, `appScope` and `excludeSelf`. Android builds its
system VPN from that object, then attaches the fd with
`GopurrStartVPNWithPlan(handle, fd, protector, token)`. Go constructs its TUN
from the exact saved plan. The token binds the active profile, source/override
revision and routing mode; stale plans fail before fd duplication or teardown.
Stop and committed source changes invalidate a pending plan. A repeated start
of the already attached plan is idempotent. Legacy `StartVPN` remains available
with the default all-app scope.

Scopes are `all`, `include` or `exclude`, with at most 256 normalized package
names. Include requires at least one package. Android resolves installed-app
identity and excludes the host itself; packages are not translated into desktop
process rules or root-only Android package-manager access.

## Validation and remaining upstream issue

The integration test uses temporary directories and loopback HTTP servers. It
checks actual proxy traffic and counters, node selection, delay testing,
credential-free summaries, failed-import isolation, activation rollback after
an index commit failure, listener release, restart, and same-home reopen with
persisted routing mode and per-profile node choices. Preference regressions
also reject corrupt/oversized/linked files, preserve linked targets, and check
that a failed save leaves the prior runtime and disk choices intact. It
does not change system proxy settings or use a real subscription or VPN.

`go test -race ./sdk/engine` exposed upstream mutable inbound ACL state during
live configuration reload. Explicitly stopping old connections/listeners before
loading removes those reports in the test. A remaining upstream race is
independent of the SDK's serialized state: mihomo v1.19.33
`hub/executor/executor.go:87` calls `log.SetLevel`, which assigns the ordinary
global `log.level` at `log/log.go:79`, while a Go GC finalizer can call
`adapter/outbound/base.go:403` → `log.Debugln` → `log.print` and read it at
`log/log.go:83`. Even a rejected configuration can allocate adapters whose
finalizers run later, outside host locks. Functional tests pass; the full race
check must not be reported as clean until that upstream logger is synchronized.
No vendored dependency or suppression is used to conceal the report.
