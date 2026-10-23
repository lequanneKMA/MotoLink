# Thiết kế giao diện (Figma Make)

`figma-make/` là bản xuất từ Figma Make (React + Tailwind, chỉ để **tham chiếu thiết kế**, không phải code chạy của app).
Xem thử: `cd docs/design-ui/figma-make && pnpm install && pnpm dev`.

Nguyên tắc thiết kế: người dùng đang chạy xe, chỉ liếc 1 giây. Bản đồ là giao diện; chỉ hai cỡ chữ (14/20 sp);
nút ≥ 56 dp; đỏ chỉ dành cho SOS và cảnh báo nguy hiểm; trạng thái không chỉ dựa vào màu.

## Màn thiết kế → Compose

| Màn trong thiết kế | Compose | Ghi chú |
|---|---|---|
| `solo` (Đi một mình) | `feature/home/HomeScreen.kt` | Bản đồ toàn màn hình, menu, tốc độ, bottom sheet ghi hành trình. Ô "Bạn muốn đi đâu?" **chưa có** |
| `ride` (Đi nhóm) | `feature/trips/RideScreen.kt` | Pill "N/M người", "Cả nhóm", banner người tụt lại (`logic/GroupWatch`), tốc độ, sheet thành viên, SOS + Tạm dừng |
| `send-sos`, `sos-sent`, `receive-sos` | `feature/sos/SosScreens.kt` | Một chạm gửi; "Chỉ đường tới" mở ứng dụng bản đồ của máy qua `geo:` |
| `trips`, `create-trip` | `feature/trips/TripsScreens.kt` | Mã mời 8 ký tự (thiết kế vẽ 6 số) |
| `settings` | `feature/settings/SettingsScreen.kt` | Các công tắc quyền phản ánh trạng thái thật, chạm để mở cài đặt hệ thống |
| `auth` | `feature/auth/AuthScreens.kt` | Thêm đăng ký (đồng ý điều khoản tách riêng) |
| Theme/thành phần | `ui/Theme.kt`, `ui/Components.kt` | Màu và cỡ chữ lấy từ `index.css` của thiết kế |

## Dẫn đường (đã làm, chưa chạy thử)

`solo` → ô "Bạn muốn đi đâu?" → `feature/nav/NavScreens.kt`: tìm địa điểm (PostGIS) → chọn tuyến (OSRM, tối đa 3 phương án) → dẫn đường từng bước
(biểu ngữ rẽ, giọng nói tiếng Việt qua TextToSpeech của máy, tự tìm lại đường khi lệch). Logic nằm ở `logic/Nav.kt` (có test). Dẫn đường chỉ chạy khi
màn hình đang mở (foreground). Hạ tầng: `deploy/maps/README.md`.

## Có trong thiết kế nhưng CHƯA làm

`navigate-alert` và `report` (cảnh báo/báo cáo cộng đồng), `route-planner` (tuyến cho đoàn);  (báo kẹt xe/ngập: cần dịch vụ báo cáo cộng đồng); `fall` (phát hiện ngã bằng cảm biến);
`group-stop`, `group-stop-other` ("Chụp ảnh/Nghỉ chân…", "Chờ bạn/Đi trước": cần loại sự kiện mới ở server);
cài đặt "Bản đồ offline", "Vòng tốc độ", "Link cho người nhà", "Giọng nói dẫn đường".
Bản đồ nền là nền trống cho tới khi bạn dựng `deploy/maps` và đặt `TILE_BASE_URL`; khi đó app tự lấy style từ `GET /v1/config`.
