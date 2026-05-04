import { useEffect, useRef } from "react";
import { BrowserOpenURL } from "../../wailsjs/runtime/runtime";
import { Toast as ToastT, ToastLevel, useToastStore } from "../stores/toastStore";

const AUTO_DISMISS_MS = 6000;

const LEVEL_CLASSES: Record<ToastLevel, string> = {
  info: "border-slate-700 bg-slate-900",
  warning: "border-amber-700 bg-amber-950/90",
  error: "border-red-700 bg-red-950/90",
  success: "border-emerald-700 bg-emerald-950/90",
};

type Props = {
  onOpenCard: (workspaceId: string, issueNumber: number) => void;
};

export function Toast({ onOpenCard }: Props) {
  const recent = useToastStore((s) => s.recent);
  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none w-80">
      {recent.map((t) => (
        <ToastItem key={t.id} toast={t} onOpenCard={onOpenCard} />
      ))}
    </div>
  );
}

function ToastItem({
  toast,
  onOpenCard,
}: {
  toast: ToastT;
  onOpenCard: (workspaceId: string, issueNumber: number) => void;
}) {
  const dismiss = useToastStore((s) => s.dismiss);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    if (toast.persist) return;
    timerRef.current = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
      }
    };
  }, [toast.id, toast.persist, dismiss]);

  function pauseTimer() {
    if (toast.persist) return;
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }

  function resumeTimer() {
    if (toast.persist) return;
    if (timerRef.current === null) {
      timerRef.current = window.setTimeout(() => dismiss(toast.id), AUTO_DISMISS_MS);
    }
  }

  async function handleClick() {
    switch (toast.action) {
      case "open_plan":
      case "focus_card":
        onOpenCard(toast.workspace_id, toast.issue_number);
        break;
      case "open_pr":
        if (toast.pr_url) BrowserOpenURL(toast.pr_url);
        break;
    }
    dismiss(toast.id);
  }

  function handleDismissClick(e: React.MouseEvent) {
    e.stopPropagation();
    dismiss(toast.id);
  }

  const levelClass = LEVEL_CLASSES[toast.level] ?? LEVEL_CLASSES.info;
  const showPRLink = toast.action === "open_pr" && !!toast.pr_url;

  return (
    <div
      role="status"
      onClick={handleClick}
      onMouseEnter={pauseTimer}
      onMouseLeave={resumeTimer}
      className={`pointer-events-auto cursor-pointer rounded border px-3 py-2 shadow-lg text-slate-200 ${levelClass}`}
    >
      <div className="flex items-start gap-2">
        <div className="flex-1 min-w-0">
          <div className="text-xs font-semibold text-slate-100 truncate">{toast.title}</div>
          <div className="text-xs text-slate-300 mt-0.5 break-words">{toast.body}</div>
          {showPRLink && (
            <div className="text-xs text-emerald-300 mt-1 underline">View PR</div>
          )}
        </div>
        <button
          type="button"
          onClick={handleDismissClick}
          aria-label="Dismiss"
          className="text-slate-500 hover:text-slate-300 text-xs leading-none"
        >
          ×
        </button>
      </div>
    </div>
  );
}
