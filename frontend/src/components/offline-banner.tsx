import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

// Lightweight offline indicator. The app shell is precached by the service
// worker, so when the network drops the UI still renders — this banner tells
// the user why writes/API calls are failing instead of leaving them guessing.
//
// On small screens the dashboard's opaque mobile header is fixed to the top
// with the same z-chrome token, so this banner must sit below the header
// (top-[var(--mobile-header-height)]) or it would be painted over. On desktop
// (lg+) the mobile header is hidden and the banner goes back to top-0.
function OfflineBanner() {
  const { t } = useTranslation();
  const [offline, setOffline] = useState(
    typeof navigator !== "undefined" ? !navigator.onLine : false
  );

  useEffect(() => {
    const onOnline = () => setOffline(false);
    const onOffline = () => setOffline(true);
    window.addEventListener("online", onOnline);
    window.addEventListener("offline", onOffline);
    return () => {
      window.removeEventListener("online", onOnline);
      window.removeEventListener("offline", onOffline);
    };
  }, []);

  if (!offline) return null;
  return (
    <div
      role="status"
      className="fixed inset-x-0 top-[var(--mobile-header-height)] z-chrome bg-warning px-4 py-1.5 text-center text-sm text-black lg:top-0"
    >
      {t("common.offline-reconnecting")}
    </div>
  );
}

export { OfflineBanner };
