# Third party notices

Gopurr is distributed under the GNU General Public License, version 3 or later.
The complete source and build instructions are at https://github.com/csic21/Gopurr.

- mihomo: https://github.com/MetaCubeX/mihomo — GPL-3.0.
- Tailscale / tsnet (MetaCubeX fork): https://github.com/MetaCubeX/tailscale
  — BSD-3-Clause; see licenses/tailscale.txt. Vendored goupnp and oauth2 notices
  are included in licenses/tailscale-goupnp.txt and licenses/tailscale-oauth2.txt.
- Offline GeoIP/GeoSite/ASN baseline: https://github.com/MetaCubeX/meta-rules-dat
  — GPL-3.0; see licenses/meta-rules-dat.txt. Source commit and database SHA-256
  hashes are recorded in internal/geodata/data/manifest.json. Upstream data
  sources and build scripts are published in that repository.
- mygo: https://github.com/egoist/mygo — MIT; its native runtime bundles the
  libraries and licenses supplied by the framework.
- Lucide icons: https://lucide.dev — ISC; see internal/app/icons/LICENSE.
- The Windows TUN implementation in sing-tun embeds Wintun 0.14.1. Wintun is
  Copyright © 2018–2021 WireGuard LLC. The official precompiled driver is
  distributed under its binary distribution license. See
  https://www.wintun.net/ and the licenses included in the sing-tun module.
- Additional Go dependencies and exact versions are recorded in go.mod/go.sum;
  their licenses are available in the corresponding upstream modules.
