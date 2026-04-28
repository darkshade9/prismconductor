export function GoalPane() {
  return (
    <div className="px-4 py-3 border-b border-slate-800">
      <div className="text-sm text-slate-300">
        <span className="text-slate-500">Active Goal:</span>{" "}
        <span className="text-amber-300">🎯 Get 2 types of each spell working</span>
      </div>
      <div className="text-xs text-slate-500 mt-1">
        Up Next: Combat polish · Admin parity &nbsp;|&nbsp; Past: Magic foundations (achieved 2026-04-15)
      </div>
    </div>
  );
}
