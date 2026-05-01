import { useEffect, useRef } from "react";
import { ClipboardSetText } from "../../wailsjs/runtime/runtime";

export type CopyAction = { label: string; text: string };

export function toQuotedBlock(text: string): string {
  return text
    .trim()
    .split("\n")
    .map((l) => `> ${l}`)
    .join("\n");
}

export function CopyMenu({
  x,
  y,
  actions,
  onClose,
}: {
  x: number;
  y: number;
  actions: CopyAction[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    // Capture-phase listener fires before any element-level handler in the
    // bubble phase, so `stopPropagation` on the menu div can't suppress it.
    // Filter by target-contained-by-menu instead. Without this, clicking a
    // copy button dismisses the menu before the button's onClick gets to
    // run — the menu opens, you click "Copy ...", and nothing lands in the
    // clipboard because the click never reaches the action handler.
    const dismiss = (e: Event) => {
      const target = e.target as Node | null;
      if (target && ref.current && ref.current.contains(target)) {
        return;
      }
      onClose();
    };
    window.addEventListener("pointerdown", dismiss, { capture: true });
    window.addEventListener("keydown", () => onClose(), { once: true });
    return () => {
      window.removeEventListener("pointerdown", dismiss, { capture: true });
    };
  }, [onClose]);

  async function copy(text: string) {
    try {
      await ClipboardSetText(text);
    } catch {
      await navigator.clipboard?.writeText(text).catch(() => {});
    }
    onClose();
  }

  return (
    <div
      ref={ref}
      className="fixed bg-slate-800 border border-slate-600 rounded shadow-xl py-1 min-w-[160px]"
      style={{
        left: Math.min(x, window.innerWidth - 180),
        top: Math.min(y, window.innerHeight - actions.length * 32 - 8),
        zIndex: 9999,
      }}
    >
      {actions.map((action, i) => (
        <button
          key={i}
          className="block w-full text-left px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700 whitespace-nowrap"
          onClick={(e) => {
            e.stopPropagation();
            copy(action.text);
          }}
        >
          {action.label}
        </button>
      ))}
    </div>
  );
}
