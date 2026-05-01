import { useEffect, useRef, useState } from "react";
import { DiagnoseIssue } from "../../wailsjs/go/main/App";
import { diagnose } from "../../wailsjs/go/models";

type Props = {
  workspaceID: string;
  issueNumber: number;
  anchor: { x: number; y: number };
  onClose: () => void;
};

export function DiagnosticPopover({ workspaceID, issueNumber, anchor, onClose }: Props) {
  const [result, setResult] = useState<diagnose.IssueDiagnosis | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    DiagnoseIssue(workspaceID, issueNumber)
      .then((d) => setResult(diagnose.IssueDiagnosis.createFrom(d)))
      .catch((e: any) => setErr(String(e?.message ?? e)));
  }, [workspaceID, issueNumber]);

  // Close on outside click.
  useEffect(() => {
    function handler(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose();
      }
    }
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, [onClose]);

  const severityColor = result
    ? result.severity_level === "ok"
      ? "text-emerald-300"
      : result.severity_level === "warn"
      ? "text-amber-300"
      : "text-red-300"
    : "text-slate-400";

  const severityBorder = result
    ? result.severity_level === "ok"
      ? "border-emerald-700"
      : result.severity_level === "warn"
      ? "border-amber-700"
      : "border-red-700"
    : "border-slate-700";

  // Position the popover so it stays on screen.
  const style: React.CSSProperties = {
    position: "fixed",
    zIndex: 100,
    left: Math.min(anchor.x, window.innerWidth - 360),
    top: anchor.y + 8,
  };

  return (
    <div
      ref={ref}
      style={style}
      className={`w-80 bg-slate-900 border ${severityBorder} rounded-lg shadow-xl text-sm`}
    >
      <div className="flex items-center justify-between px-3 py-2 border-b border-slate-800">
        <span className="text-xs text-slate-400 font-mono">
          Why is #{issueNumber} stuck?
        </span>
        <button
          onClick={onClose}
          className="text-slate-500 hover:text-slate-300 text-xs leading-none"
        >
          ✕
        </button>
      </div>

      <div className="px-3 py-2 space-y-2">
        {!result && !err && (
          <div className="text-slate-500 text-xs animate-pulse">Diagnosing…</div>
        )}

        {err && (
          <div className="text-red-400 text-xs">{err}</div>
        )}

        {result && (
          <>
            <div className={`font-medium text-xs ${severityColor}`}>
              {severityIcon(result.severity_level)} {result.summary}
            </div>

            {result.detail.length > 0 && (
              <ul className="space-y-0.5">
                {result.detail.map((line, i) => (
                  <li key={i} className="text-xs text-slate-300 font-mono leading-tight">
                    {line}
                  </li>
                ))}
              </ul>
            )}

            {result.suggestion && (
              <div className="pt-1 border-t border-slate-800 text-xs text-slate-400">
                → {result.suggestion}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function severityIcon(level: string): string {
  switch (level) {
    case "ok": return "✓";
    case "warn": return "⚠";
    case "stuck": return "✕";
    default: return "ⓘ";
  }
}
