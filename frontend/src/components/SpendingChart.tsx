// Minimal SVG sparkline for the SpendingPanel trend chart.
// Renders a path through up to 30 daily data points.

type Point = { label: string; value: number };

export function Sparkline({
  points,
  width = 200,
  height = 40,
  color = "#38bdf8",
}: {
  points: Point[];
  width?: number;
  height?: number;
  color?: string;
}) {
  if (points.length < 2) {
    return (
      <svg width={width} height={height} className="text-slate-700">
        <line x1={0} y1={height / 2} x2={width} y2={height / 2} stroke="currentColor" strokeWidth={1} strokeDasharray="4 2" />
      </svg>
    );
  }

  const values = points.map((p) => p.value);
  const maxVal = Math.max(...values, 0.001);
  const pad = 4;
  const usableW = width - pad * 2;
  const usableH = height - pad * 2;

  const coords = points.map((p, i) => {
    const x = pad + (i / (points.length - 1)) * usableW;
    const y = pad + usableH - (p.value / maxVal) * usableH;
    return [x, y] as [number, number];
  });

  const d = coords
    .map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`)
    .join(" ");

  // Fill path: close to bottom-right then bottom-left.
  const [lastX] = coords[coords.length - 1];
  const [firstX] = coords[0];
  const fillD = `${d} L${lastX.toFixed(1)},${(pad + usableH).toFixed(1)} L${firstX.toFixed(1)},${(pad + usableH).toFixed(1)} Z`;

  return (
    <svg width={width} height={height} className="overflow-visible">
      <path d={fillD} fill={color} fillOpacity={0.12} />
      <path d={d} stroke={color} strokeWidth={1.5} fill="none" strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
}
