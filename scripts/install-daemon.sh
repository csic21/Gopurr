#!/bin/sh
# Thin wrapper so the script can be run by hand from the repo; the app embeds
# internal/service/install.sh instead of shipping this file.
exec /bin/sh "$(dirname "$0")/../internal/service/install.sh" "$@"
