# First-launch geodata

The compressed files in `data/` are an offline baseline, not a pinned runtime
database. Both user and privileged kernels prepare their own missing/invalid
files before parsing a subscription. Valid existing files are preserved.

`make update-geodata` resolves the MetaCubeX `release` branch to a single commit,
downloads all files from that commit, validates them and produces deterministic
gzip assets plus `data/manifest.json`. Formal release builds refresh them
automatically. No developer profile or cache is used to produce these assets.

Once the daemon is ready, its background worker updates only enabled databases
through mihomo's current outbound routing. It honors the usual `geox-url`,
`geo-auto-update` and `geo-update-interval` settings, with daily updates enabled
by default. The documented upstream CDN is a fallback only for standard sources.
Custom URLs never fall back to a different dataset.

Downloads have a timeout and size cap. Validation happens before an atomic
replacement, and config reloads are serialized with commits so an old in-flight
download cannot replace a new profile's database. Successful checks persist
across restarts, including unchanged content. Failed checks keep the current
files and retry after an hour; reloads wake the worker and apply source/policy
changes immediately.

Source and build scripts: <https://github.com/MetaCubeX/meta-rules-dat>.
License: `licenses/meta-rules-dat.txt` (GPL-3.0).
