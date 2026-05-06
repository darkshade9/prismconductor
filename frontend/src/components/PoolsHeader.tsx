import { useEffect, useState } from "react";
import { EventsOnWrapped } from "../lib/eventBus";
import { ListPools } from "../../wailsjs/go/main/App";
import { workerpool } from "../../wailsjs/go/models";
import { resolveProviderIcon } from "../lib/providerIcon";
import { getPoolSummaryByRole, ProviderSummary } from "../stores/usePoolsStore";

type Props = { onOpenPools: () => void };

export function PoolsHeader({ onOpenPools }: Props) {
  const [pools, setPools] = useState<workerpool.PoolStatus[]>([]);

  useEffect(() => {
    const refresh = () =>
      ListPools()
        .then((rows) => setPools(rows ?? []))
        .catch(() => {});
    refresh();
    const offFreed = EventsOnWrapped("bus.worker_slot_freed", refresh);
    const offChanged = EventsOnWrapped("bus.agent_count_changed", refresh);
    const offState = EventsOnWrapped("session.state", refresh);
    const offPending = EventsOnWrapped("bus.pending_pool_enqueued", refresh);
    const offDequeued = EventsOnWrapped("bus.pending_pool_dequeued", refresh);
    return () => {
      if (typeof offFreed === "function") offFreed();
      if (typeof offChanged === "function") offChanged();
      if (typeof offState === "function") offState();
      if (typeof offPending === "function") offPending();
      if (typeof offDequeued === "function") offDequeued();
    };
  }, []);

  const summary = getPoolSummaryByRole(pools);
  if (summary.length === 0) return null;

  return (
    <div
      className="fixed bottom-0 left-0 right-0 z-50 px-4 py-1 border-t border-slate-800 flex items-center gap-4 cursor-pointer hover:bg-slate-900/40"
      onClick={onOpenPools}
      role="button"
      aria-label="Open pool settings"
      tabIndex={0}
      onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && onOpenPools()}
    >
      {summary.map((roleSummary) => (
        <div
          key={roleSummary.role}
          className="flex items-center gap-1.5 text-xs"
        >
          <span className="text-slate-500">{roleSummary.label}:</span>
          <div className="flex items-center gap-1">
            {roleSummary.providers.map((entry, i) => (
              <span key={entry.kind} className="inline-flex items-center gap-0.5">
                {i > 0 && <span className="text-slate-700 mx-0.5">·</span>}
                <ProviderEntry entry={entry} />
              </span>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

function ProviderEntry({ entry }: { entry: ProviderSummary }) {
  const icon = resolveProviderIcon(entry.kind);
  const isSaturated = entry.capacity > 0 && entry.active >= entry.capacity;
  const countClass = isSaturated ? "text-amber-400" : "text-slate-300";
  const budgetLines = entry.budgets
    .filter((b) => b.max_input_tokens || b.max_turns)
    .map((b) => {
      const parts: string[] = [];
      if (b.max_input_tokens) parts.push(`${b.max_input_tokens.toLocaleString()} tok in`);
      if (b.max_turns) parts.push(`${b.max_turns} turns`);
      return `  ${b.name}: ${parts.join(", ")}`;
    });
  const tooltip = [
    `${entry.active}/${entry.capacity} — pools: ${entry.poolNames.join(", ")}`,
    ...budgetLines,
  ].join("\n");
  const ariaLabel = `${icon.label} providers: ${entry.active} of ${entry.capacity} active`;

  return (
    <span
      className="inline-flex items-center gap-0.5"
      title={tooltip}
      aria-label={ariaLabel}
    >
      <span
        className="inline-block text-slate-400"
        style={{ width: 16, height: 16 }}
        dangerouslySetInnerHTML={{ __html: icon.svgContent }}
        aria-hidden="true"
      />
      <span className={`font-mono ${countClass}`}>
        {entry.active}/{entry.capacity}
      </span>
    </span>
  );
}
