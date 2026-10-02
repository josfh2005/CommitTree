#!/bin/bash
# Wails post-build hook (darwin/*, wails.json): compiles assets/CommitTree.icon
# into Assets.car inside the built .app, so macOS 26+ shows the icon at full
# size instead of shrinking it onto a grey tile as it does with icns-only
# icons. iconfile.icns stays as the fallback for older systems. Runs in
# build/bin; $1 is the built app or its binary.
set -euo pipefail
cd "$(dirname "$0")/.."
app="$1"
while [[ "$app" != *.app && "$app" != / ]]; do app="$(dirname "$app")"; done
[[ "$app" == *.app ]] || app="build/bin/CommitTree.app"
if ! xcrun --find actool >/dev/null 2>&1; then
  echo "mac-icon: actool not found (install Xcode); keeping the icns icon" >&2
  exit 0
fi
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
xcrun actool assets/CommitTree.icon --compile "$out" --app-icon CommitTree \
  --platform macosx --minimum-deployment-target 11.0 --target-device mac \
  --output-partial-info-plist "$out/partial.plist" >/dev/null
cp "$out/Assets.car" "$app/Contents/Resources/"
codesign --force --deep -s - "$app"
