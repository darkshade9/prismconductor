import { useEffect } from "react";
import { useSettingsStore } from "../stores/useSettingsStore";

const isMac = /Mac|Macintosh|iPhone|iPad/.test(
  typeof navigator !== "undefined" ? (navigator.platform || navigator.userAgent) : "",
);

export function useDiagnosticUnlock() {
  const setDiagnosticsUnlocked = useSettingsStore((s) => s.setDiagnosticsUnlocked);

  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      const modifierOk = isMac ? e.metaKey : e.ctrlKey;
      if (modifierOk && e.altKey && e.key.toLowerCase() === "d") {
        setDiagnosticsUnlocked(true);
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [setDiagnosticsUnlocked]);
}
