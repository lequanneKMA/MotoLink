#!/usr/bin/env bash
# Builds everything the self-hosted map stack needs from ONE OpenStreetMap extract of Vietnam:
#   1. OSRM routing graph          -> maps/data/vietnam-latest.osrm*
#   2. OpenMapTiles vector tiles   -> maps/data/vietnam.mbtiles   (planetiler)
#   3. fonts for map labels        -> maps/fonts/*.ttf
#   4. place-search table          -> PostGIS database `osm`, table `places`
# Needs Docker, ~10 GB free disk, ~6 GB RAM for a few minutes (OSRM extract and planetiler), and internet.
# Re-run after a few weeks to refresh the data. Safe to run again (steps overwrite their outputs).
set -euo pipefail
cd "$(dirname "$0")"
mkdir -p data fonts
PBF=data/vietnam-latest.osm.pbf
COMPOSE="docker compose -f ../docker-compose.yml"

if [ ! -f "$PBF" ] || [ -n "$(find "$PBF" -mtime +30 2>/dev/null)" ]; then
  echo "== downloading OSM extract (Geofabrik)"
  curl -fL --retry 3 -o "$PBF" https://download.geofabrik.de/asia/vietnam-latest.osm.pbf
fi

echo "== 1/4 OSRM graph (car profile; motorways are excluded per request because motorbikes may not use them)"
OSRM=ghcr.io/project-osrm/osrm-backend
docker run --rm -t -v "$PWD/data:/data" $OSRM osrm-extract -p /opt/car.lua /data/vietnam-latest.osm.pbf
docker run --rm -t -v "$PWD/data:/data" $OSRM osrm-partition /data/vietnam-latest.osrm
docker run --rm -t -v "$PWD/data:/data" $OSRM osrm-customize /data/vietnam-latest.osrm

echo "== 2/4 vector tiles (planetiler, OpenMapTiles schema)"
docker run --rm -e JAVA_TOOL_OPTIONS="-Xmx4g" -v "$PWD/data:/data" ghcr.io/onthegomap/planetiler:latest \
  --download --osm-path=/data/vietnam-latest.osm.pbf --output=/data/vietnam.mbtiles --force

echo "== 3/4 fonts"
base=https://github.com/notofonts/notofonts.github.io/raw/main/fonts/NotoSans/hinted/ttf
for f in NotoSans-Regular NotoSans-Bold; do [ -f "fonts/$f.ttf" ] || curl -fL --retry 3 -o "fonts/$f.ttf" "$base/$f.ttf"; done

echo "== 4/4 place search (osm2pgsql -> PostGIS -> places)"
$COMPOSE --profile maps up -d osmdb
until $COMPOSE exec -T osmdb pg_isready -U osm -d osm >/dev/null 2>&1; do sleep 2; done
NET=$($COMPOSE ps -q osmdb | xargs docker inspect -f '{{range $k,$v := .NetworkSettings.Networks}}{{$k}}{{end}}')
docker run --rm --network "$NET" -v "$PWD/data:/data" -e PGPASSWORD=osm debian:bookworm-slim bash -c '
  apt-get update -qq && apt-get install -y -qq osm2pgsql >/dev/null &&
  osm2pgsql --create --slim --drop --cache 2000 -d osm -U osm -H osmdb --hstore-all -l /data/vietnam-latest.osm.pbf'
$COMPOSE exec -T osmdb psql -U osm -d osm -v ON_ERROR_STOP=1 -f - < search.sql

echo "done. Start the stack:  docker compose -f ../docker-compose.yml --profile maps up -d"
