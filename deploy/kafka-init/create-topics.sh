#!/bin/bash
# Creates topics per docs/design/09-kafka.md §2. Idempotent (--if-not-exists).
set -euo pipefail
BS="${BOOTSTRAP:-kafka:9092}"
K=/opt/kafka/bin/kafka-topics.sh
mk() { # name partitions retention_ms [retention_bytes]
  local args=(--bootstrap-server "$BS" --create --if-not-exists --topic "$1" --partitions "$2" --replication-factor 1 --config "retention.ms=$3")
  [ -n "${4:-}" ] && args+=(--config "retention.bytes=$4")
  "$K" "${args[@]}"
}
DAY=86400000
mk motolink.location.v1            3 $((3*DAY))  2147483648
mk motolink.location.v1.dlq        1 $((14*DAY))
mk motolink.trip-events.v1         3 $((7*DAY))
mk motolink.trip-events.v1.dlq     1 $((14*DAY))
mk motolink.sos.v1                 3 $((14*DAY))
mk motolink.sos.v1.dlq             1 $((30*DAY))
"$K" --bootstrap-server "$BS" --list
