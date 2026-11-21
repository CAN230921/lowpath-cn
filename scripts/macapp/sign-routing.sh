#!/bin/sh
# Embed and sign the Developer ID routing extension in a staged macapp bundle.
set -eu

if [ "$#" -ne 5 ]; then
    echo "Usage: $0 Lowpath.app 'Developer ID Application: ...' TEAM_ID App.provisionprofile Route.provisionprofile" >&2
    exit 2
fi
app=$1
identity=$2
team=$3
app_profile=$4
extension_profile=$5
sources=$(CDPATH= cd -- "$(dirname -- "$0")/network-extension" && pwd)
info="$app/Contents/Info.plist"
bundle=$(/usr/libexec/PlistBuddy -c 'Print CFBundleIdentifier' "$info")
version=$(/usr/libexec/PlistBuddy -c 'Print CFBundleVersion' "$info")
exe=$(/usr/libexec/PlistBuddy -c 'Print CFBundleExecutable' "$info")
id="$bundle.routing"
group="$team.$bundle"
extension="$app/Contents/Library/SystemExtensions/$id.systemextension"
ensure_identifier() {
    case "$1" in
        ''|*[!A-Za-z0-9.-]*) return 1 ;;
    esac
}
ensure_identifier "$id"
ensure_identifier "$team"
ensure_identifier "$version"
[ -r "$app_profile" ] && [ -r "$extension_profile" ]
[ "$identity" != "-" ]
[ ! -e "$extension" ]

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
architectures=$(/usr/bin/lipo -archs "$app/Contents/MacOS/$exe")
/bin/mkdir "$scratch/Contents" "$scratch/Contents/MacOS"
native_files=
for architecture in $architectures; do
    case "$architecture" in
        arm64|x86_64) ;;
        *) echo "Unknown Mac architecture: $architecture" >&2; exit 1 ;;
    esac
    /usr/bin/xcrun swiftc -O -target "$architecture-apple-macosx15.0" -module-name LowpathRoute -lbsm \
        "$sources/Proxy.swift" "$sources/main.swift" -o "$scratch/$architecture"
    native_files="$native_files $architecture"
done
(
    cd "$scratch"
    # Each architecture is validated above; lipo accepts one input as well.
    /usr/bin/lipo -create $native_files -output "Contents/MacOS/$id"
)
cat > "$scratch/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key><string>$id</string>
    <key>CFBundleExecutable</key><string>$id</string>
    <key>CFBundlePackageType</key><string>SYSX</string>
    <key>CFBundleName</key><string>Lowpath Routing</string>
    <key>CFBundleVersion</key><string>$version</string>
    <key>CFBundleShortVersionString</key><string>$version</string>
    <key>LSMinimumSystemVersion</key><string>15.0</string>
    <key>NetworkExtension</key><dict>
        <key>NEMachServiceName</key><string>$group.routing</string>
        <key>NEProviderClasses</key><dict>
            <key>com.apple.networkextension.app-proxy</key><string>LowpathRoute.TransparentProxy</string>
        </dict>
    </dict>
</dict>
</plist>
PLIST
for target in app extension; do
    install_permission=
    if [ "$target" = app ]; then
        install_permission='<key>com.apple.developer.system-extension.install</key><true/>'
    fi
    cat > "$scratch/$target.entitlements" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.developer.networking.networkextension</key>
    <array><string>app-proxy-provider-systemextension</string></array>
    <key>com.apple.security.application-groups</key><array><string>$group</string></array>
    $install_permission
</dict>
</plist>
PLIST
done
/bin/mkdir -p "$app/Contents/Library/SystemExtensions" "$scratch/$id.systemextension"
mv "$scratch/Contents" "$scratch/$id.systemextension/"
cp "$extension_profile" "$scratch/$id.systemextension/Contents/embedded.provisionprofile"
/usr/bin/codesign --force --options runtime --timestamp --sign "$identity" \
    --entitlements "$scratch/extension.entitlements" "$scratch/$id.systemextension"
mv "$scratch/$id.systemextension" "$extension"
cp "$app_profile" "$app/Contents/embedded.provisionprofile"
/usr/bin/codesign --force --options runtime --timestamp --sign "$identity" --entitlements "$scratch/app.entitlements" "$app"
/usr/bin/codesign --verify --deep --strict "$app"
