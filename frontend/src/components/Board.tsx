import { Column } from "./Column";
import { Card } from "./Card";

// Phase 1 stub: hard-coded sample cards. Replaced by real data once GitHub fetch lands.
export function Board({ onCardClick }: { onCardClick?: (n: number) => void }) {
  return (
    <div className="flex gap-3 px-4 pb-4 overflow-x-auto">
      <Column title="TODO" count={3}>
        <Card number={1116} workspace="pe-eng" title="Foundation: spell schema" state="primitive" priority={1} color="#22c55e" onClick={() => onCardClick?.(1116)} />
        <Card number={1117} workspace="pe-eng" title="Foundation: damage table" state="primitive" priority={0.95} color="#22c55e" />
        <Card number={1131} workspace="pe-eng" title="GSX magic crit polish" state="blocked" blockedBy={1116} color="#22c55e" />
      </Column>
      <Column title="PLAN" count={1}>
        <Card number={1130} workspace="pe-eng" title="GSX magic crit table refactor" state="plan_ready" color="#22c55e" />
      </Column>
      <Column title="IN_PROGRESS" count={1}>
        <Card number={1145} workspace="editor" title="Editor: snap-to-grid" state="working" color="#06b6d4" />
      </Column>
      <Column title="REVIEW" count={1}>
        <Card number={1099} workspace="pe-eng" title="Inventory weight cap" state="pr_open" color="#22c55e" />
      </Column>
      <Column title="DONE" count={0} />
    </div>
  );
}
