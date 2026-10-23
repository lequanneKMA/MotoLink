import { useEffect, useState } from "react";

type Screen =
  | "solo"
  | "route"
  | "navigate"
  | "navigate-alert"
  | "report"
  | "fall"
  | "ride"
  | "send-sos"
  | "sos-sent"
  | "receive-sos"
  | "trips"
  | "create-trip"
  | "settings"
  | "auth"
  | "group-stop"
  | "group-stop-other"
  | "route-planner";

type IconName =
  | "group"
  | "pause"
  | "play"
  | "close"
  | "route"
  | "check"
  | "plus"
  | "settings"
  | "back"
  | "share"
  | "moon"
  | "sun"
  | "chevron"
  | "menu"
  | "voice"
  | "muted"
  | "locate"
  | "mic"
  | "warning"
  | "turn"
  | "camera"
  | "fuel";

function Icon({ name, size = 28 }: { name: IconName; size?: number }) {
  const paths: Record<IconName, React.ReactNode> = {
    group: (
      <>
        <circle cx="9" cy="8" r="3" />
        <circle cx="17" cy="9" r="2.5" />
        <path d="M3.5 19c.5-4 2.5-6 5.5-6s5 2 5.5 6M14 14c3.2-.7 5.4 1 6 4" />
      </>
    ),
    pause: (
      <>
        <path d="M7 5h4v14H7zM14 5h4v14h-4z" fill="currentColor" stroke="none" />
      </>
    ),
    play: <path d="m8 5 11 7-11 7V5Z" fill="currentColor" stroke="none" />,
    close: <path d="m6 6 12 12M18 6 6 18" />,
    route: (
      <>
        <circle cx="6" cy="18" r="2" />
        <circle cx="18" cy="6" r="2" />
        <path d="M8 18h3a2 2 0 0 0 2-2v-8a2 2 0 0 1 2-2h1" />
      </>
    ),
    check: <path d="m5 12 4 4L19 6" />,
    plus: <path d="M12 5v14M5 12h14" />,
    settings: (
      <>
        <circle cx="12" cy="12" r="3" />
        <path d="M19 13.5v-3l-2-.7-.7-1.7.9-1.9-2.1-2.1-1.9.9-1.7-.7-.7-2h-3l-.7 2-1.7.7-1.9-.9-2.1 2.1.9 1.9-.7 1.7-2 .7v3l2 .7.7 1.7-.9 1.9 2.1 2.1 1.9-.9 1.7.7.7 2h3l.7-2 1.7-.7 1.9.9 2.1-2.1-.9-1.9.7-1.7 2-.7Z" />
      </>
    ),
    back: <path d="m15 5-7 7 7 7" />,
    share: (
      <>
        <circle cx="18" cy="5" r="2" />
        <circle cx="6" cy="12" r="2" />
        <circle cx="18" cy="19" r="2" />
        <path d="m8 11 8-5M8 13l8 5" />
      </>
    ),
    moon: <path d="M19 16.5A8 8 0 0 1 8 5a8 8 0 1 0 11 11.5Z" />,
    sun: (
      <>
        <circle cx="12" cy="12" r="4" />
        <path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5 19 19M19 5l-1.5 1.5M6.5 17.5 5 19" />
      </>
    ),
    chevron: <path d="m9 6 6 6-6 6" />,
    menu: <path d="M4 6h16M4 12h16M4 18h16" />,
    voice: (
      <>
        <path d="M5 10v4h3l4 4V6L8 10H5Z" />
        <path d="M16 9c1.5 1.5 1.5 4.5 0 6M19 6c3.5 3.5 3.5 8.5 0 12" />
      </>
    ),
    muted: (
      <>
        <path d="M5 10v4h3l4 4V6L8 10H5Z" />
        <path d="m16 9 5 6M21 9l-5 6" />
      </>
    ),
    locate: (
      <>
        <circle cx="12" cy="12" r="6" />
        <circle cx="12" cy="12" r="2" fill="currentColor" stroke="none" />
        <path d="M12 2v3M12 19v3M2 12h3M19 12h3" />
      </>
    ),
    mic: (
      <>
        <rect x="9" y="3" width="6" height="11" rx="3" />
        <path d="M6 11a6 6 0 0 0 12 0M12 17v4M9 21h6" />
      </>
    ),
    warning: (
      <>
        <path d="M12 3 2.5 20h19L12 3Z" />
        <path d="M12 9v5M12 17h.01" />
      </>
    ),
    turn: <path d="M5 19V9a4 4 0 0 1 4-4h9M14 2l4 3-4 3" />,
    camera: (
      <>
        <rect x="3" y="6" width="18" height="14" rx="2" />
        <circle cx="12" cy="13" r="4" />
        <path d="m8 6 1.5-2h5L16 6" />
      </>
    ),
    fuel: (
      <>
        <path d="M5 21V4h9v17M3 21h13M7 8h5" />
        <path d="m14 7 4 3v7a2 2 0 0 0 4 0v-6l-2-2" />
      </>
    ),
  };

  return (
    <svg
      aria-hidden="true"
      className="icon"
      fill="none"
      height={size}
      viewBox="0 0 24 24"
      width={size}
    >
      <g stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.2">
        {paths[name]}
      </g>
    </svg>
  );
}

const members = [
  { initial: "B", name: "Bạn", status: "Trực tiếp · 52 km/h", className: "member-you" },
  { initial: "L", name: "Linh", status: "Trực tiếp · 49 km/h", className: "member-linh" },
  { initial: "M", name: "Minh", status: "30 giây trước", className: "member-minh" },
  { initial: "A", name: "An", status: "Đang dừng chụp ảnh · 3 phút", className: "member-an" },
  { initial: "H", name: "Hùng", status: "Mất tín hiệu 2 phút", className: "member-hung" },
  { initial: "T", name: "Tuấn", status: "Ngoại tuyến", className: "member-tuan" },
];

function Map({
  alert = false,
  compact = false,
  solo = false,
  route = false,
  alternatives = false,
  hazard = false,
}: {
  alert?: boolean;
  compact?: boolean;
  solo?: boolean;
  route?: boolean;
  alternatives?: boolean;
  hazard?: boolean;
}) {
  return (
    <div className={`map ${compact ? "map-compact" : ""}`} aria-label="Bản đồ vị trí nhóm">
      <svg className="map-art" viewBox="0 0 412 915" preserveAspectRatio="xMidYMid slice">
        <rect className="map-base" width="412" height="915" />
        <path className="map-park" d="M-20 72 135 25l36 172-154 55Z" />
        <path className="map-water" d="M340-20c-15 135 29 202 10 333s-71 183-45 328 102 188 84 320h70V-20Z" />
        <g className="map-minor">
          <path d="M-20 340 430 470M-10 708 326 294M55 930 280-20M-20 542 430 640" />
          <path d="M115-10 147 930M-20 820 360 530M20 166 340 315" />
        </g>
        <g className="map-road-edge">
          <path d="M-30 755C72 684 112 601 194 539s110-85 241-104" />
          <path d="M28-20c24 151 91 231 128 343s20 214 80 326 117 139 167 174" />
        </g>
        <g className="map-road">
          <path d="M-30 755C72 684 112 601 194 539s110-85 241-104" />
          <path d="M28-20c24 151 91 231 128 343s20 214 80 326 117 139 167 174" />
        </g>
        {alternatives && (
          <g className="route-alternatives">
            <path d="M72 790C152 704 120 590 202 515s124-140 155-270" />
            <path d="M72 790c34-142 151-126 174-256s16-219 101-315" />
          </g>
        )}
        {route && (
          <path
            className="active-route"
            d="M72 790c48-84 110-123 122-232s-14-185 65-245 88-94 98-154"
          />
        )}
        <g className="road-labels">
          <text x="188" y="520">QL 20</text>
          <text x="158" y="270">ĐT 725</text>
        </g>
      </svg>

      {!compact && !solo && (
        <>
          <div className="place place-one">Tà Nung</div>
          <div className="place place-two">Nam Ban</div>
          <div className="marker marker-you">
            <span className="direction">▲</span>
          </div>
          <div className="marker marker-linh">L</div>
          <div className="marker marker-minh">M</div>
          <div className="marker marker-an">A</div>
          <div className={`marker marker-hung ${alert ? "is-alert" : ""}`}>H</div>
          <div className="marker-label marker-hung-label">
            {alert ? "1,2 km · 2 phút" : "Hùng"}
          </div>
          <div className="marker marker-tuan">T</div>
        </>
      )}

      {!compact && solo && (
        <>
          <div className="place place-one">Quận 3</div>
          <div className="place place-two">Quận 1</div>
          <div className="marker marker-you solo-position">
            <span className="direction">▲</span>
          </div>
          {hazard && (
            <div className="hazard-pin">
              <Icon name="warning" size={30} />
            </div>
          )}
        </>
      )}

      {compact && (
        <>
          <div className="marker marker-help">H</div>
          <div className="pulse-ring" />
          <div className="marker-label help-label">Hùng · QL 20</div>
        </>
      )}
    </div>
  );
}

function BackHeader({
  title,
  onBack,
  action,
}: {
  title: string;
  onBack: () => void;
  action?: React.ReactNode;
}) {
  return (
    <div className="page-header">
      <button className="icon-button" aria-label="Quay lại" onClick={onBack}>
        <Icon name="back" />
      </button>
      <div className="page-title">{title}</div>
      <div className="header-action">{action}</div>
    </div>
  );
}

function SpeedDial({ speed = "0" }: { speed?: string }) {
  return (
    <div className="speed-dial" aria-label={`${speed} ki-lô-mét một giờ`}>
      <strong>{speed}</strong>
      <span>km/h</span>
    </div>
  );
}

function MapSos({ onClick }: { onClick: () => void }) {
  return (
    <button className="map-sos" onClick={onClick} aria-label="Gửi SOS">
      SOS
    </button>
  );
}

function ReportButton({ onClick }: { onClick: () => void }) {
  return (
    <button className="report-fab" onClick={onClick} aria-label="Báo tình trạng đường">
      <span className="report-triangle">+</span>
      <span>Báo</span>
    </button>
  );
}

function ModeSwitch({
  mode,
  setScreen,
}: {
  mode: "solo" | "group";
  setScreen: (screen: Screen) => void;
}) {
  return (
    <div className="mode-switch" aria-label="Chọn chế độ đi">
      <button
        className={mode === "solo" ? "active" : ""}
        onClick={() => setScreen("solo")}
      >
        Đi một mình
      </button>
      <button
        className={mode === "group" ? "active" : ""}
        onClick={() => setScreen("ride")}
      >
        Đi nhóm
      </button>
    </div>
  );
}

function SoloHome({
  dark,
  setDark,
  setScreen,
}: {
  dark: boolean;
  setDark: (value: boolean) => void;
  setScreen: (screen: Screen) => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);
  const [voice, setVoice] = useState(true);

  return (
    <main className={`solo-screen ${dark ? "theme-dark" : "theme-light"}`}>
      <Map solo />
      <button className="map-control menu-control" onClick={() => setMenuOpen(true)} aria-label="Mở menu">
        <Icon name="menu" size={30} />
      </button>
      <div className="home-speed"><SpeedDial /></div>
      <button className="map-control voice-control" onClick={() => setVoice(!voice)} aria-label="Bật tắt giọng nói">
        <Icon name={voice ? "voice" : "muted"} size={30} />
      </button>
      <button className="map-control locate-control" aria-label="Căn lại vị trí">
        <Icon name="locate" size={30} />
      </button>

      <div className="solo-sheet">
        <span className="sheet-handle" />
        <ModeSwitch mode="solo" setScreen={setScreen} />
        <button className="destination-search" onClick={() => setScreen("route")}>
          <span>Bạn muốn đi đâu?</span>
          <Icon name="mic" size={30} />
        </button>
        <div className="shortcut-row">
          <button>Nhà riêng</button>
          <button>Cơ quan</button>
          <button>+ Mới</button>
        </div>
      </div>

      <div className="solo-map-actions">
        <MapSos onClick={() => setScreen("send-sos")} />
        <ReportButton onClick={() => setScreen("report")} />
      </div>

      {menuOpen && (
        <div className="menu-scrim" onClick={() => setMenuOpen(false)}>
          <aside className="side-menu" onClick={(event) => event.stopPropagation()}>
            <div className="side-menu-title">MotoLink</div>
            <button onClick={() => setDark(!dark)}>
              <span className="menu-icon"><Icon name={dark ? "sun" : "moon"} /></span>
              <span>{dark ? "Bản đồ sáng" : "Bản đồ tối"}</span>
            </button>
            <button onClick={() => setScreen("fall")}>
              <span className="menu-icon"><Icon name="warning" /></span>
              <span>Thử phát hiện ngã</span>
            </button>
            <button onClick={() => setScreen("trips")}>
              <span className="menu-icon"><Icon name="group" /></span>
              <span>Chuyến đi của tôi</span>
            </button>
            <button onClick={() => setScreen("settings")}>
              <span className="menu-icon"><Icon name="settings" /></span>
              <span>Cài đặt</span>
            </button>
            <button className="side-menu-close" onClick={() => setMenuOpen(false)}>Đóng</button>
          </aside>
        </div>
      )}
    </main>
  );
}

function RouteSelection({
  dark,
  setScreen,
}: {
  dark: boolean;
  setScreen: (screen: Screen) => void;
}) {
  const [selected, setSelected] = useState(1);
  const routes = [
    ["Nhanh nhất", "25 phút", ""],
    ["Đường tắt", "17 phút", "-8 phút"],
    ["Ít kẹt", "21 phút", "-4 phút"],
  ];
  return (
    <main className={`solo-screen ${dark ? "theme-dark" : "theme-light"}`}>
      <Map solo route alternatives />
      <div className="route-sheet">
        <span className="sheet-handle" />
        <button className="route-cancel" onClick={() => setScreen("solo")}>Hủy</button>
        <div className="route-options">
          {routes.map(([name, time, delta], index) => (
            <button
              className={selected === index ? "selected" : ""}
              key={name}
              onClick={() => setSelected(index)}
            >
              <span><strong>{name}</strong><small>{delta}</small></span>
              <strong>{time}</strong>
            </button>
          ))}
        </div>
        <button className="start-route" onClick={() => setScreen("navigate")}>Bắt đầu</button>
      </div>
    </main>
  );
}

function NavigationScreen({
  dark,
  alert,
  setScreen,
}: {
  dark: boolean;
  alert?: boolean;
  setScreen: (screen: Screen) => void;
}) {
  const [confirm, setConfirm] = useState(false);
  return (
    <main className={`solo-screen ${dark ? "theme-dark" : "theme-light"}`}>
      <Map solo route hazard={alert} />
      <button
        className="turn-banner"
        onClick={() => !alert && setScreen("navigate-alert")}
        aria-label="Hướng rẽ tiếp theo"
      >
        <Icon name="turn" size={48} />
        <div><strong>300 m</strong><span>Rẽ phải vào Nguyễn Đình Chiểu</span></div>
      </button>
      {alert && (
        <div className="road-alert">
          <Icon name="warning" size={28} />
          <span>Ngập nước phía trước · 400 m</span>
        </div>
      )}
      <div className={`nav-speed ${alert ? "pushed" : ""}`}><SpeedDial speed="42" /></div>
      <div className="nav-voice"><Icon name="voice" size={30} /></div>
      <div className="navigation-actions">
        <MapSos onClick={() => setScreen("send-sos")} />
        <ReportButton onClick={() => setScreen("report")} />
      </div>
      {alert && (
        <div className="hazard-confirm">
          <span>{confirm ? "Đã cập nhật · Cảm ơn bạn" : "Tình trạng ngập nước?"}</span>
          {!confirm && (
            <div>
              <button onClick={() => setConfirm(true)}>Còn</button>
              <button onClick={() => setConfirm(true)}>Hết</button>
            </div>
          )}
        </div>
      )}
      <div className="nav-bottom">
        <strong>12 phút · 4,3 km</strong>
        <button onClick={() => setScreen("solo")}>Kết thúc</button>
      </div>
    </main>
  );
}

function ReportScreen({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const [sent, setSent] = useState(false);
  const reports = [
    ["Kẹt xe", "route"],
    ["Tai nạn", "warning"],
    ["Nguy hiểm", "warning"],
    ["Ngập nước", "route"],
    ["Đóng đường", "close"],
    ["Đường hỏng / ổ gà", "warning"],
    ["Thời tiết xấu", "warning"],
    ["Khác", "plus"],
  ] as [string, IconName][];
  return (
    <main className="page report-page">
      <div className="report-map"><Map solo route /></div>
      <section className="report-sheet">
        <span className="sheet-handle" />
        <div className="report-title">{sent ? "Đã báo · Cảm ơn bạn" : "Bạn thấy gì?"}</div>
        {!sent ? (
          <>
            <div className="report-grid">
              {reports.map(([label, icon], index) => (
                <button
                  className={index === 1 || index === 2 ? "danger-report" : ""}
                  key={label}
                  onClick={() => {
                    setSent(true);
                    window.setTimeout(() => setScreen("navigate"), 2000);
                  }}
                >
                  <span><Icon name={icon} size={34} /></span>
                  <strong>{label}</strong>
                </button>
              ))}
            </div>
            <button className="report-cancel" onClick={() => setScreen("navigate")}>Hủy</button>
          </>
        ) : (
          <div className="report-thanks"><Icon name="check" size={56} /></div>
        )}
      </section>
    </main>
  );
}

function FallScreen({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const [count, setCount] = useState(10);
  useEffect(() => {
    if (count === 0) {
      setScreen("sos-sent");
      return;
    }
    const timer = window.setTimeout(() => setCount((value) => value - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [count, setScreen]);
  return (
    <main className="fall-page">
      <div className="fall-warning"><Icon name="warning" size={64} /></div>
      <div className="fall-title">Bạn ổn không?</div>
      <div className="fall-count" aria-label="Đếm ngược gửi SOS">
        {count}
      </div>
      <div className="fall-note">Tự gửi SOS Tai nạn khi hết giờ</div>
      <button className="im-ok" onClick={() => setScreen("solo")}>Tôi ổn</button>
    </main>
  );
}

function RideScreen({
  dark,
  setDark,
  setScreen,
}: {
  dark: boolean;
  setDark: (value: boolean) => void;
  setScreen: (screen: Screen) => void;
}) {
  const [sheetOpen, setSheetOpen] = useState(false);
  const [lagAlert, setLagAlert] = useState(false);

  return (
    <main className={`ride-screen ${dark ? "theme-dark" : "theme-light"}`}>
      <Map alert={lagAlert} route />

      <div className={`top-controls ${lagAlert ? "with-alert" : ""}`}>
        <button
          className="group-status"
          onClick={() => setSheetOpen(true)}
          aria-label="Mở danh sách 6 thành viên"
        >
          <span className="online-dot" />
          <strong>5/6 người · còn 42 km</strong>
        </button>
        <button className="fit-group" onClick={() => setLagAlert((value) => !value)}>
          <Icon name="group" size={26} />
          <span>Xem cả nhóm</span>
        </button>
      </div>

      {lagAlert && (
        <button className="lag-alert" onClick={() => setSheetOpen(true)}>
          <span className="warning-mark">!</span>
          <span>Hùng cách 1,2 km · mất tín hiệu 2 phút</span>
          <Icon name="chevron" size={24} />
        </button>
      )}

      <button
        className="theme-toggle"
        aria-label={dark ? "Chuyển sang bản đồ sáng" : "Chuyển sang bản đồ tối"}
        onClick={() => setDark(!dark)}
      >
        <Icon name={dark ? "sun" : "moon"} />
      </button>
      <div className={`group-speed ${lagAlert ? "pushed" : ""}`}><SpeedDial speed="52" /></div>

      <div className={`member-sheet ${sheetOpen ? "is-open" : ""}`}>
        <button
          className="sheet-handle-area"
          aria-label={sheetOpen ? "Thu gọn danh sách" : "Mở danh sách thành viên"}
          onClick={() => setSheetOpen(!sheetOpen)}
        >
          <span className="sheet-handle" />
          {!sheetOpen && <span className="sheet-summary">5 đang trực tuyến</span>}
        </button>

        {sheetOpen && (
          <div className="member-list">
            <div className="sheet-title-row">
              <div className="sheet-title">Nhóm Đà Lạt tháng 3</div>
              <button className="sheet-close" aria-label="Đóng" onClick={() => setSheetOpen(false)}>
                <Icon name="close" size={24} />
              </button>
            </div>
            {members.map((member) => (
              <button
                className="member-row"
                key={member.name}
                onClick={() => {
                  if (member.name === "Hùng" && lagAlert) setScreen("receive-sos");
                  else if (member.name === "Hùng") setLagAlert(true);
                  else if (member.name === "An") setScreen("group-stop-other");
                }}
              >
                <span className={`member-avatar ${member.className}`}>{member.initial}</span>
                <span className="member-copy">
                  <strong>{member.name}</strong>
                  <span>{member.status}</span>
                </span>
                {member.name === "Hùng" && (
                  <span className={`signal-state ${lagAlert ? "danger" : ""}`}>
                    {lagAlert ? "Cần kiểm tra" : "1,2 km"}
                  </span>
                )}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="ride-actions">
        <button className="sos-button" onClick={() => setScreen("send-sos")}>
          <span className="sos-ring">SOS</span>
        </button>
        <button className="pause-button" onClick={() => setScreen("group-stop")}>
          <Icon name="pause" size={30} />
          <span>Tạm dừng</span>
        </button>
      </div>
    </main>
  );
}

function SendSos({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const reasons = ["Hỏng xe", "Tai nạn", "Bị lạc", "Cần y tế", "Khác"];
  return (
    <main className="page sos-select-page">
      <BackHeader title="Bạn cần giúp gì?" onBack={() => setScreen("ride")} />
      <div className="sos-help">Chạm một lần để gửi ngay cho cả nhóm</div>
      <div className="reason-list">
        {reasons.map((reason, index) => (
          <button
            className={`reason-button ${index === 1 || index === 3 ? "urgent" : ""}`}
            key={reason}
            onClick={() => setScreen("sos-sent")}
          >
            <span className="reason-symbol">{["XE", "!", "?", "+", "…"][index]}</span>
            <strong>{reason}</strong>
            <Icon name="chevron" />
          </button>
        ))}
      </div>
      <button className="cancel-button" onClick={() => setScreen("ride")}>
        Hủy
      </button>
    </main>
  );
}

function SosSent({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const [offline, setOffline] = useState(false);
  return (
    <main className="page sent-page">
      <div className="sent-content">
        <div className={`sent-icon ${offline ? "offline" : ""}`}>
          <Icon name={offline ? "route" : "check"} size={54} />
        </div>
        <div className="sent-title">
          {offline ? "Chưa có mạng" : "Đã gửi cho 5 người"}
        </div>
        <div className="sent-subtitle">
          {offline ? "Sẽ tự gửi khi có sóng" : "Cả nhóm đã nhận được vị trí của bạn"}
        </div>
      </div>
      <button className="primary-button" onClick={() => setScreen("ride")}>
        Về bản đồ
      </button>
      <button className="text-button" onClick={() => setOffline(!offline)}>
        {offline ? "Thử gửi lại" : "Xem trạng thái mất mạng"}
      </button>
    </main>
  );
}

function ReceiveSos({ setScreen }: { setScreen: (screen: Screen) => void }) {
  return (
    <main className="receive-page">
      <div className="receive-copy">
        <div className="receive-title">HÙNG CẦN GIÚP</div>
        <div className="receive-reason">Hỏng xe</div>
      </div>
      <Map compact />
      <div className="receive-actions">
        <button className="receive-primary" onClick={() => setScreen("ride")}>
          <Icon name="route" size={30} />
          <span>Chỉ đường tới</span>
        </button>
        <button className="receive-secondary" onClick={() => setScreen("ride")}>
          <Icon name="check" size={30} />
          <span>Đã biết</span>
        </button>
      </div>
    </main>
  );
}

function GroupStop({
  setScreen,
  other = false,
  dark = false,
}: {
  setScreen: (screen: Screen) => void;
  other?: boolean;
  dark?: boolean;
}) {
  const [selected, setSelected] = useState("");
  const options: [string, IconName][] = [
    ["Chụp ảnh", "camera"],
    ["Nghỉ chân", "pause"],
    ["Đi vệ sinh", "locate"],
    ["Đổ xăng", "fuel"],
  ];
  if (other) {
    return (
      <main className={`ride-screen stopped-member-screen ${dark ? "theme-dark" : "theme-light"}`}>
        <Map route />
        <div className="info-banner member-stop-banner">
          Hùng dừng chụp ảnh · cách 800 m
        </div>
        <div className="marker-label stopped-member-label">
          <Icon name="pause" size={22} /> Hùng · đang dừng
        </div>
        <MapSos onClick={() => setScreen("send-sos")} />
        <div className="member-response">
          <button onClick={() => setScreen("ride")}>Chờ bạn</button>
          <button onClick={() => setScreen("ride")}>Đi trước</button>
        </div>
      </main>
    );
  }
  return (
    <main className="page group-stop-page">
      <div className="info-banner">Bạn đang dừng? Báo cho đoàn</div>
      <div className="stop-content">
        <div className="stopped-title">{selected ? "Đã báo cho đoàn" : "Bạn dừng để làm gì?"}</div>
        {selected ? (
          <div className="stop-confirmed">
            <span className="stop-confirm-icon"><Icon name="check" size={52} /></span>
            <strong>{selected}</strong>
            <span>Mọi người sẽ thấy bạn vẫn an toàn</span>
          </div>
        ) : (
          <div className="stop-grid">
            {options.map(([label, icon]) => (
              <button key={label} onClick={() => setSelected(label)}>
                <span><Icon name={icon} size={38} /></span>
                <strong>{label}</strong>
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="stop-actions">
        <button className="primary-button" onClick={() => setScreen("ride")}>
          {selected ? "Trở lại bản đồ" : "Tiếp tục hành trình"}
        </button>
        <button className="secondary-button" onClick={() => setScreen("trips")}>Kết thúc chuyến</button>
      </div>
    </main>
  );
}

function Trips({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const trips = [
    ["Đà Lạt tháng 3", "Đang đi · 5 người trực tuyến"],
    ["Cung Hà Giang", "Đã kết thúc"],
    ["Biển Hồ Trị An", "Đã kết thúc"],
  ];
  return (
    <main className="page stopped-page">
      <div className="stopped-header">
        <div>
          <div className="eyebrow">MOTOLINK</div>
          <div className="stopped-title">Chuyến đi</div>
        </div>
        <button className="icon-button" aria-label="Cài đặt" onClick={() => setScreen("settings")}>
          <Icon name="settings" />
        </button>
      </div>
      <div className="trip-list">
        {trips.map(([name, status], index) => (
          <button className="trip-row" key={name} onClick={() => setScreen("ride")}>
            <span className={`trip-symbol trip-${index + 1}`}>{name.slice(0, 1)}</span>
            <span className="trip-copy">
              <strong>{name}</strong>
              <span>{status}</span>
            </span>
            <Icon name="chevron" />
          </button>
        ))}
      </div>
      <div className="resume-banner">
        <span>Đang tạm dừng chia sẻ</span>
        <button onClick={() => setScreen("ride")}>
          <Icon name="play" size={22} />
          Tiếp tục
        </button>
      </div>
      <button className="route-plan-entry" onClick={() => setScreen("route-planner")}>
        <span className="menu-icon"><Icon name="route" /></span>
        <span><strong>Tạo tuyến cho đoàn</strong><small>Điểm đến và các điểm dừng</small></span>
        <Icon name="chevron" />
      </button>
      <button className="fab" aria-label="Tạo hoặc tham gia chuyến đi" onClick={() => setScreen("create-trip")}>
        <Icon name="plus" size={32} />
      </button>
    </main>
  );
}

function RoutePlanner({ setScreen }: { setScreen: (screen: Screen) => void }) {
  return (
    <main className="page">
      <BackHeader title="Tạo tuyến đoàn" onBack={() => setScreen("trips")} />
      <div className="form-content route-planner">
        <label className="field-label" htmlFor="destination">Điểm đến</label>
        <input className="text-field" id="destination" placeholder="Tìm địa điểm" />
        <div className="section-label">ĐIỂM DỪNG</div>
        <div className="waypoint"><span>1</span><strong>Đèo Bảo Lộc</strong><button>Đổi</button></div>
        <div className="waypoint"><span>2</span><strong>Hồ Xuân Hương</strong><button>Đổi</button></div>
        <button className="add-stop"><Icon name="plus" /> Thêm điểm dừng</button>
        <button className="primary-button" onClick={() => setScreen("ride")}>Gửi cho cả đoàn</button>
      </div>
    </main>
  );
}

function CreateTrip({ setScreen }: { setScreen: (screen: Screen) => void }) {
  const [joined, setJoined] = useState(false);
  return (
    <main className="page">
      <BackHeader title="Thêm chuyến đi" onBack={() => setScreen("trips")} />
      {!joined ? (
        <div className="form-content">
          <label className="field-label" htmlFor="trip-name">Tên chuyến đi</label>
          <input className="text-field" id="trip-name" placeholder="Ví dụ: Đà Lạt cuối tuần" />
          <button className="primary-button" onClick={() => setJoined(true)}>Tạo chuyến mới</button>
          <div className="or-rule"><span>hoặc</span></div>
          <label className="field-label" htmlFor="invite-code">Mã mời</label>
          <input className="text-field invite-input" id="invite-code" placeholder="Nhập mã 6 số" />
          <button className="secondary-button" onClick={() => setJoined(true)}>Tham gia chuyến</button>
        </div>
      ) : (
        <div className="invite-content">
          <div className="invite-label">MÃ MỜI CỦA NHÓM</div>
          <div className="invite-code">482 913</div>
          <div className="invite-note">Đọc mã này cho người muốn tham gia</div>
          <button className="primary-button">
            <Icon name="share" size={26} />
            Chia sẻ
          </button>
          <button className="secondary-button" onClick={() => setScreen("ride")}>Vào bản đồ nhóm</button>
        </div>
      )}
    </main>
  );
}

function SwitchRow({
  label,
  description,
  defaultOn = true,
}: {
  label: string;
  description: string;
  defaultOn?: boolean;
}) {
  const [on, setOn] = useState(defaultOn);
  return (
    <button className="setting-row" role="switch" aria-checked={on} onClick={() => setOn(!on)}>
      <span className="setting-copy">
        <strong>{label}</strong>
        <span>{description}</span>
      </span>
      <span className={`switch ${on ? "on" : ""}`}><span /></span>
    </button>
  );
}

function Settings({ setScreen }: { setScreen: (screen: Screen) => void }) {
  return (
    <main className="page">
      <BackHeader title="Cài đặt và quyền" onBack={() => setScreen("trips")} />
      <div className="settings-list">
        <div className="section-label">CHIA SẺ VỊ TRÍ</div>
        <SwitchRow label="Vị trí chính xác" description="Cho phép khi đang đi cùng nhóm" />
        <SwitchRow label="Chạy trong nền" description="Giữ vị trí khi tắt màn hình" />
        <SwitchRow label="Chia sẻ tốc độ" description="Hiển thị tốc độ cho cả đoàn" />
        <div className="section-label">CẢNH BÁO</div>
        <SwitchRow label="Người bị tụt lại" description="Báo khi cách nhóm hơn 1 km" />
        <SwitchRow label="Âm thanh SOS" description="Luôn phát âm thanh lớn" />
        <SwitchRow label="Giọng nói dẫn đường" description="Đọc hướng rẽ khi đang đi" />
        <SwitchRow label="Bản đồ offline" description="Tải trước khi mất mạng" />
        <SwitchRow label="Vòng tốc độ" description="Hiển thị trên bản đồ" />
        <div className="section-label">AN TOÀN</div>
        <SwitchRow label="Người nhận SOS" description="5 thành viên trong đoàn" />
        <SwitchRow label="Link cho người nhà" description="Cho phép xem vị trí chuyến đi" defaultOn={false} />
        <div className="section-label">TÀI KHOẢN</div>
        <button className="setting-row account-row" onClick={() => setScreen("auth")}>
          <span className="setting-copy">
            <strong>Tài khoản</strong>
            <span>Đăng nhập để đồng bộ chuyến đi</span>
          </span>
          <Icon name="chevron" />
        </button>
      </div>
    </main>
  );
}

function Auth({ setScreen }: { setScreen: (screen: Screen) => void }) {
  return (
    <main className="page auth-page">
      <BackHeader title="Đăng nhập" onBack={() => setScreen("settings")} />
      <div className="auth-brand">
        <div className="brand-mark">M</div>
        <div className="stopped-title">MotoLink</div>
        <div className="auth-subtitle">Giữ liên lạc trên mọi cung đường</div>
      </div>
      <div className="auth-form">
        <label className="field-label" htmlFor="email">Email</label>
        <input className="text-field" id="email" type="email" placeholder="ban@email.com" />
        <label className="field-label" htmlFor="password">Mật khẩu</label>
        <input className="text-field" id="password" type="password" placeholder="Ít nhất 8 ký tự" />
        <button className="primary-button" onClick={() => setScreen("trips")}>Đăng nhập</button>
        <button className="text-button">Tạo tài khoản mới</button>
      </div>
    </main>
  );
}

export default function App() {
  const [screen, setScreen] = useState<Screen>("solo");
  const [dark, setDark] = useState(false);

  return (
    <div className={`app-shell ${dark ? "app-theme-dark" : ""}`}>
      {screen === "solo" && <SoloHome dark={dark} setDark={setDark} setScreen={setScreen} />}
      {screen === "route" && <RouteSelection dark={dark} setScreen={setScreen} />}
      {screen === "navigate" && <NavigationScreen dark={dark} setScreen={setScreen} />}
      {screen === "navigate-alert" && <NavigationScreen dark={dark} alert setScreen={setScreen} />}
      {screen === "report" && <ReportScreen setScreen={setScreen} />}
      {screen === "fall" && <FallScreen setScreen={setScreen} />}
      {screen === "ride" && <RideScreen dark={dark} setDark={setDark} setScreen={setScreen} />}
      {screen === "send-sos" && <SendSos setScreen={setScreen} />}
      {screen === "sos-sent" && <SosSent setScreen={setScreen} />}
      {screen === "receive-sos" && <ReceiveSos setScreen={setScreen} />}
      {screen === "trips" && <Trips setScreen={setScreen} />}
      {screen === "create-trip" && <CreateTrip setScreen={setScreen} />}
      {screen === "settings" && <Settings setScreen={setScreen} />}
      {screen === "auth" && <Auth setScreen={setScreen} />}
      {screen === "group-stop" && <GroupStop setScreen={setScreen} dark={dark} />}
      {screen === "group-stop-other" && <GroupStop setScreen={setScreen} other dark={dark} />}
      {screen === "route-planner" && <RoutePlanner setScreen={setScreen} />}
    </div>
  );
}
