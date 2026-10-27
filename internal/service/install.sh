#!/bin/sh
# Runs only after explicit administrator authorization. Persistent executables,
# provider caches and logs live beneath a root-owned, non-writable directory.
set -eu
PATH=/usr/bin:/bin:/usr/sbin:/sbin
export PATH

ACTION=${1:-install}
BIN=${2:-}
DIR=${3:-"$HOME/Library/Application Support/Gopurr"}
LABEL=${4:-io.github.gopurr.core}
[ "$LABEL" = io.github.gopurr.core ] || { echo "无效的服务名称" >&2; exit 2; }
PLIST="/Library/LaunchDaemons/$LABEL.plist"
SERVICE_PARENT=/Library/PrivilegedHelperTools
SERVICE_DIR="$SERVICE_PARENT/$LABEL"
TARGET="$SERVICE_DIR/Gopurr-core"

# Never follow an existing symlink or take over a user-owned service directory.
protected_dir() {
    [ ! -L "$1" ] || { echo "服务目录不能是链接: $1" >&2; exit 1; }
    if [ ! -e "$1" ]; then mkdir "$1"; chown root:wheel "$1"; fi
    [ -d "$1" ] && [ "$(stat -f '%u' "$1")" = 0 ] || { echo "服务目录必须归 root 所有: $1" >&2; exit 1; }
    chmod "$2" "$1"
}
xml_escape() {
    printf '%s' "$1" | sed -e 's/\&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g' -e 's/"/\&quot;/g' -e "s/'/\&apos;/g"
}

case "$ACTION" in
uninstall)
    launchctl bootout "system/$LABEL" 2>/dev/null || true
    rm -f "$PLIST"
    # This fixed directory is protected; never delete paths supplied by the UI.
    [ ! -L "$SERVICE_DIR" ] || { echo "服务目录不能是链接" >&2; exit 1; }
    rm -rf "$SERVICE_DIR"
    echo "已卸载 $LABEL"
    exit 0
    ;;
install) ;;
*) echo "未知操作: $ACTION" >&2; exit 2 ;;
esac

[ -n "$BIN" ] && [ -f "$BIN" ] && [ -x "$BIN" ] || { echo "找不到可执行文件" >&2; exit 1; }
case "$BIN" in
*AppTranslocation* | /Volumes/*)
    echo "请先把应用移到 /Applications 再安装服务。" >&2; exit 1 ;;
esac
[ -f "$DIR/runtime.yaml" ] || { echo "先运行一次应用生成配置" >&2; exit 1; }
[ -d "$DIR" ] && [ ! -L "$DIR" ] || { echo "数据目录不能是链接" >&2; exit 1; }
# The GUI tightens this directory as its owner; root must not chmod a
# user-controlled path (which could be swapped for a symlink during install).
protected_dir "$SERVICE_PARENT" 755
protected_dir "$SERVICE_DIR" 700

STAGED=$(mktemp "$SERVICE_DIR/.core-XXXXXX")
STAGED_PLIST=$(mktemp "$SERVICE_DIR/.plist-XXXXXX")
trap 'rm -f "$STAGED" "$STAGED_PLIST"' EXIT HUP INT TERM
cp "$BIN" "$STAGED"
chown root:wheel "$STAGED"
chmod 755 "$STAGED"

CONFIG=$(xml_escape "$DIR/runtime.yaml")
SOCK=$(xml_escape "$DIR/core.sock")
SECRET=$(xml_escape "$DIR/core.secret")
STATUS=$(xml_escape "$DIR/core.json")
cat >"$STAGED_PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$TARGET</string>
    <string>--core</string>
    <string>--config</string><string>$CONFIG</string>
    <string>--socket</string><string>$SOCK</string>
    <string>--secret-file</string><string>$SECRET</string>
    <string>--home</string><string>$SERVICE_DIR/mihomo</string>
    <string>--status</string><string>$STATUS</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><false/>
  <key>StandardOutPath</key><string>$SERVICE_DIR/core.log</string>
  <key>StandardErrorPath</key><string>$SERVICE_DIR/core.log</string>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
PLIST
chown root:wheel "$STAGED_PLIST"
chmod 644 "$STAGED_PLIST"
launchctl bootout "system/$LABEL" 2>/dev/null || true
mv -f "$STAGED" "$TARGET"
mv -f "$STAGED_PLIST" "$PLIST"
launchctl bootstrap system "$PLIST"
echo "已安装 $LABEL"
