import { useEffect } from "react";
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
  useEffect(() => {
    const dismiss = () => onClose();
    window.addEventListener("pointerdown", dismiss, { capture: true, once: true });
    window.addEventListener("keydown", dismiss, { once: true });
    return () => {
      window.removeEventListener("pointerdown", dismiss, { capture: true });
      window.removeEventListener("keydown", dismiss);
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
      className="fixed bg-slate-800 border border-slate-600 rounded shadow-xl py-1 min-w-[160px]"
      style={{
        left: Math.min(x, window.innerWidth - 180),
        top: Math.min(y, window.innerHeight - actions.length * 32 - 8),
        zIndex: 9999,
      }}
      onPointerDown={(e) => e.stopPropagation()}
      onClick={(e) => e.stopPropagation()}
    >
      {actions.map((action, i) => (
        <button
          key={i}
          className="block w-full text-left px-3 py-1.5 text-xs text-slate-200 hover:bg-slate-700 whitespace-nowrap"
          onClick={() => copy(action.text)}
        >
          {action.label}
        </button>
      ))}
    </div>
  );
}
