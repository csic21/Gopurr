#!/bin/sh
# Installs Gopurr's kernel as a LaunchDaemon (root), which is what TUN needs.
#
# The app calls this only when the user asks for it, inside one administrator
# authorization prompt. It is also safe to run by hand:
#
#   sudo ./scripts/install-daemon.sh install /path/to/Gopurr "$HOME/Library/Application Support/gopurr"
#   sudo ./scripts/install-daemon.sh uninstall
#
# Positional: action [binary] [data dir] [label]
set -eu

ACTION=${1:-install}
BIN=${2:-}
DIR=${3:-"$HOME/Library/Application Support/gopurr"}
LABEL=${4:-io.github.gopurr.core}

PLIST="/Library/LaunchDaemons/$LABEL.plist"
SOCK="$DIR/core.sock"
SECRET="$DIR/core.secret"
CONFIG="$DIR/runtime.yaml"
HOME_DIR="$DIR/mihomo"
STATUS="$DIR/core.json"
LOG="$DIR/core.log"

case "$ACTION" in
uninstall)
	launchctl bootout "system/$LABEL" 2>/dev/null || true
	rm -f "$PLIST"
	rm -f "$SOCK" "$STATUS"
	echo "已卸载 $LABEL"
	exit 0
	;;
install) ;;
*)
	echo "未知操作：$ACTION" >&2
	exit 2
	;;
esac

if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
	echo "找不到可执行文件：${BIN:-<空>}" >&2
	exit 1
fi

# A binary running from a mounted DMG or an App Translocation path cannot be
# used by launchd once the volume goes away.
case "$BIN" in
*AppTranslocation* | /Volumes/*)
	echo "应用正在从临时挂载路径运行（$BIN）。请先把它移到 /Applications 再安装服务。" >&2
	exit 1
	;;
esac

if [ ! -f "$CONFIG" ]; then
	echo "缺少 $CONFIG：先运行一次应用生成配置，再安装服务。" >&2
	exit 1
fi

mkdir -p "$HOME_DIR"
chmod 755 "$DIR" "$HOME_DIR"

cat >"$PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN</string>
    <string>--core</string>
    <string>--config</string><string>$CONFIG</string>
    <string>--socket</string><string>$SOCK</string>
    <string>--secret-file</string><string>$SECRET</string>
    <string>--home</string><string>$HOME_DIR</string>
    <string>--status</string><string>$STATUS</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
  <key>StandardOutPath</key><string>$LOG</string>
  <key>StandardErrorPath</key><string>$LOG</string>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
PLIST

chown root:wheel "$PLIST"
chmod 644 "$PLIST"

launchctl bootout "system/$LABEL" 2>/dev/null || true
launchctl bootstrap system "$PLIST"
echo "已安装 $LABEL"
