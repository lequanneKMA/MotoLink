-- Builds the table the server searches (internal/maps/geocode.go) from the osm2pgsql import.
-- PostGIS is only needed here, at import time: queries use plain lat/lng columns + trigram/unaccent indexes.
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS unaccent;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

DROP TABLE IF EXISTS places;
CREATE TABLE places AS
SELECT name, kind, rank, lat, lng, lower(unaccent(name)) AS name_norm FROM (
  -- named places and points of interest
  SELECT name,
         COALESCE(tags->'place', tags->'amenity', tags->'tourism', tags->'shop', 'poi') AS kind,
         CASE WHEN tags->'place' IN ('city','town') THEN 1 WHEN tags->'place' IS NOT NULL THEN 2 ELSE 3 END AS rank,
         ST_Y(ST_Transform(way, 4326)) AS lat, ST_X(ST_Transform(way, 4326)) AS lng
  FROM planet_osm_point
  WHERE name IS NOT NULL AND (tags ?| ARRAY['place','amenity','tourism','shop'])
  UNION ALL
  -- named areas (parks, hotels, markets, administrative areas...) by their centre
  SELECT name,
         COALESCE(tags->'place', tags->'amenity', tags->'tourism', tags->'leisure', tags->'shop', 'area'),
         CASE WHEN tags->'place' IN ('city','town') THEN 1 WHEN tags->'place' IS NOT NULL THEN 2 ELSE 3 END,
         ST_Y(ST_Transform(ST_PointOnSurface(way), 4326)), ST_X(ST_Transform(ST_PointOnSurface(way), 4326))
  FROM planet_osm_polygon
  WHERE name IS NOT NULL AND (tags ?| ARRAY['place','amenity','tourism','leisure','shop'])
  UNION ALL
  -- named roads: one row per ~1 km cell, so "Nguyễn Huệ" exists once per town instead of once for the country
  SELECT DISTINCT ON (name, round(lat::numeric, 2), round(lng::numeric, 2)) name, 'road', 4, lat, lng FROM (
    SELECT name, ST_Y(ST_Transform(ST_LineInterpolatePoint(way, 0.5), 4326)) AS lat, ST_X(ST_Transform(ST_LineInterpolatePoint(way, 0.5), 4326)) AS lng
    FROM planet_osm_line
    WHERE name IS NOT NULL AND highway IN ('motorway','trunk','primary','secondary','tertiary','residential','unclassified','living_street','pedestrian')
  ) roads
) all_places
WHERE lat BETWEEN 8 AND 24 AND lng BETWEEN 102 AND 110;

CREATE INDEX places_name_trgm ON places USING gin (name_norm gin_trgm_ops);
ANALYZE places;
