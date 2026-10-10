#!/usr/bin/env bash
# Runs the pure-Kotlin :logic tests without the Android SDK or Google Maven: builds a throw-away project that
# only contains :logic (the real root build applies the Android Gradle Plugin, which needs Google Maven).
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cp -r "$here/gradle" "$here/gradlew" "$here/gradle.properties" "$here/logic" "$tmp/"
cat > "$tmp/settings.gradle.kts" <<'KTS'
pluginManagement { repositories { mavenCentral(); gradlePluginPortal() } }
dependencyResolutionManagement { repositories { mavenCentral() } }
rootProject.name = "logic-only"
include(":logic")
KTS
cd "$tmp"
./gradlew :logic:test --console=plain "$@"
