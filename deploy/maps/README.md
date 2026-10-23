# Bản đồ tự host (MapLibre + OSM + OSRM + PostGIS)

```
OSM extract (Geofabrik)
   ├─ planetiler  → vietnam.mbtiles → Martin (:3000)   → bản đồ nền cho MapLibre (style do server sinh: GET /v1/map/style/{light|dark})
   ├─ osrm-extract → graph          → osrm-routed(:5000) → server proxy POST /v1/route
   └─ osm2pgsql    → PostGIS (osmdb) → bảng places       → server GET /v1/geocode (tìm địa điểm, gõ không dấu cũng ra)
```

## Chạy
```bash
cd deploy/maps && ./prepare.sh                      # lần đầu và khi muốn làm mới dữ liệu (≈ 20–40 phút, cần ~10 GB đĩa, ~6 GB RAM)
```
Trong `deploy/.env` thêm (thay `192.168.1.10` bằng IP LAN của máy chạy Docker — **điện thoại phải truy cập được địa chỉ này**):
```
OSRM_URL=http://osrm:5000
TILE_BASE_URL=http://192.168.1.10:3000
GEOCODE_DATABASE_URL=postgres://osm:osm@osmdb:5432/osm?sslmode=disable
```
```bash
docker compose -f deploy/docker-compose.yml --profile maps up -d --build
curl localhost:8080/v1/map/style/light | head -c 200     # style JSON
curl localhost:3000/catalog | head -c 200                 # Martin thấy vietnam
```

## Ghi chú
- Chưa chạy thử trong môi trường soạn code (không có Docker/Geofabrik). Tên image, cờ lệnh của planetiler/Martin/osm2pgsql
  và `exclude=motorway` của OSRM **cần kiểm khi chạy lần đầu**; server tự thử lại không `exclude` nếu OSRM từ chối.
- Xe máy: dùng profile `car` và loại đường cao tốc theo từng yêu cầu. Đường hẻm xe máy đi được nhưng ô tô thì không sẽ chưa được tính.
- Không có dữ liệu kẹt xe, nên không có tuyến "ít kẹt".
- Khi chuyển lên VPS: chạy lại `prepare.sh` ở đó (hoặc copy thư mục `data/`), mở cổng 3000 qua reverse proxy HTTPS và đổi `TILE_BASE_URL`.
- Dữ liệu © OpenStreetMap contributors (ODbL): giữ dòng ghi công trên bản đồ.
