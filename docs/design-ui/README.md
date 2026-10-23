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

## Có trong thiết kế nhưng CHƯA làm (cần backend/engine riêng)

`route`, `navigate`, `navigate-alert`, `route-planner` (tìm đường, dẫn đường từng bước, giọng nói, tuyến cho đoàn: cần routing engine
và nguồn tile, chưa chọn — D-15); `report` (báo kẹt xe/ngập: cần dịch vụ báo cáo cộng đồng); `fall` (phát hiện ngã bằng cảm biến);
`group-stop`, `group-stop-other` ("Chụp ảnh/Nghỉ chân…", "Chờ bạn/Đi trước": cần loại sự kiện mới ở server);
cài đặt "Bản đồ offline", "Vòng tốc độ", "Link cho người nhà", "Giọng nói dẫn đường".
Bản đồ hiện là nền trống (chưa có tile) nên chưa giống bản vẽ có đường phố.
