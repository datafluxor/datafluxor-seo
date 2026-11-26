import { ReactNode, useEffect, useState } from "react";
import { ApiError, signupUrl } from "../api";

export function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  const e = error as ApiError;
  const needsKey = e.code === "NO_API_KEY" || e.code === "INSUFFICIENT_CREDITS" || e.code === "UNAUTHORIZED";
  return (
    <div className="alert alert-error" role="alert">
      <span>{e.message ?? String(error)}</span>
      {needsKey && (
        <>
          {" "}
          <a href={e.signupUrl ?? signupUrl} target="_blank" rel="noreferrer">
            {e.code === "INSUFFICIENT_CREDITS" ? "Add credits" : "Get a free DataFluxor API key"}
          </a>
          {e.code === "NO_API_KEY" && (
            <>
              {" "}
              then paste it in <a href="/settings">Settings</a>.
            </>
          )}
        </>
      )}
      {e.retryAfter ? <span className="muted"> Try again in {e.retryAfter} s.</span> : null}
    </div>
  );
}

// KeyNotice explains that a feature needs a DataFluxor key, with the sign-up link.
export function KeyNotice({ hasKey, children }: { hasKey: boolean; children?: ReactNode }) {
  if (hasKey) return null;
  return (
    <div className="alert alert-key">
      <strong>This feature needs a DataFluxor API key.</strong> {children ?? "Live Google data comes from the DataFluxor API."}{" "}
      <a href={signupUrl} target="_blank" rel="noreferrer">
        Get a free key
      </a>{" "}
      (new accounts include starter credits), then paste it in <a href="/settings">Settings</a>.
    </div>
  );
}

export function Cost({ text }: { text: string }) {
  return <span className="cost" title="Charged to your DataFluxor credits">{text}</span>;
}

export function Spinner({ label }: { label?: string }) {
  return (
    <span className="spinner" role="status">
      <span className="spin" aria-hidden="true" />
      {label}
    </span>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>;
}

export function Position({ value }: { value: number | null | undefined }) {
  if (value === undefined) return <span className="muted">–</span>;
  if (value === null) return <span className="muted" title="Not in the results that were read">Not found</span>;
  const cls = value <= 3 ? "pos pos-top" : value <= 10 ? "pos pos-good" : "pos";
  return <span className={cls}>{value}</span>;
}

export function Change({ prev, cur }: { prev: number | null | undefined; cur: number | null | undefined }) {
  if (prev === undefined || cur === undefined) return null;
  if (prev === null && cur === null) return null;
  if (prev === null) return <span className="up">new</span>;
  if (cur === null) return <span className="down">lost</span>;
  const d = prev - cur;
  if (d === 0) return <span className="muted">=</span>;
  return <span className={d > 0 ? "up" : "down"}>{d > 0 ? `▲${d}` : `▼${-d}`}</span>;
}

// useLoad fetches on mount and when deps change; reload() fetches again.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    setLoading(true);
    fn()
      .then((d) => live && (setData(d), setError(null)))
      .catch((e) => live && setError(e))
      .finally(() => live && setLoading(false));
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  return { data, error, loading, reload: () => setTick((t) => t + 1), setData };
}

// DataTable renders the first list of records in an API answer, whatever its
// shape, so every DataFluxor tool has a readable result.
export function DataTable({ rows, max = 200 }: { rows: Record<string, unknown>[]; max?: number }) {
  if (!rows.length) return <Empty>No rows.</Empty>;
  const cols: string[] = [];
  for (const r of rows.slice(0, 20)) {
    for (const [k, v] of Object.entries(r)) {
      if (cols.includes(k) || cols.length >= 10) continue;
      if (v === null || ["string", "number", "boolean"].includes(typeof v)) cols.push(k);
    }
  }
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            {cols.map((c) => (
              <th key={c}>{c.replace(/_/g, " ")}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.slice(0, max).map((r, i) => (
            <tr key={i}>
              {cols.map((c) => (
                <td key={c}>{cell(r[c])}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {rows.length > max && <p className="muted">Showing {max} of {rows.length}.</p>}
    </div>
  );
}

function cell(v: unknown) {
  if (v === null || v === undefined) return <span className="muted">–</span>;
  if (typeof v === "number") return v.toLocaleString(undefined, { maximumFractionDigits: 2 });
  if (typeof v === "boolean") return v ? "yes" : "no";
  const s = String(v);
  if (/^https?:\/\//.test(s))
    return (
      <a href={s} target="_blank" rel="noreferrer noopener" className="url">
        {s.replace(/^https?:\/\/(www\.)?/, "")}
      </a>
    );
  return s;
}

// firstRecords finds the largest list of objects in an answer.
export function firstRecords(data: unknown): { path: string; rows: Record<string, unknown>[] } | null {
  let best: { path: string; rows: Record<string, unknown>[] } | null = null;
  const walk = (v: unknown, path: string, depth: number) => {
    if (depth > 4 || v === null || typeof v !== "object") return;
    if (Array.isArray(v)) {
      if (v.length && typeof v[0] === "object" && v[0] !== null && !Array.isArray(v[0])) {
        if (!best || v.length > best.rows.length) best = { path, rows: v as Record<string, unknown>[] };
      }
      return;
    }
    for (const [k, x] of Object.entries(v as Record<string, unknown>)) walk(x, path ? `${path}.${k}` : k, depth + 1);
  };
  walk(data, "", 0);
  return best;
}

// Scalars shows the simple top-level facts of an answer.
export function Scalars({ data, skip = [] }: { data: unknown; skip?: string[] }) {
  if (!data || typeof data !== "object") return null;
  const entries = Object.entries(data as Record<string, unknown>).filter(
    ([k, v]) => !skip.includes(k) && (typeof v === "number" || typeof v === "string" || typeof v === "boolean") && String(v).length < 120,
  );
  if (!entries.length) return null;
  return (
    <dl className="facts">
      {entries.slice(0, 16).map(([k, v]) => (
        <div key={k}>
          <dt>{k.replace(/_/g, " ")}</dt>
          <dd>{typeof v === "number" ? v.toLocaleString() : String(v)}</dd>
        </div>
      ))}
    </dl>
  );
}

// Sparkline draws a position history (lower is better, so the axis is flipped).
export function PositionChart({ points, depth }: { points: { at: string; pos: number | null }[]; depth: number }) {
  if (points.length < 2) return <p className="muted">The chart appears after two checks.</p>;
  const w = 640,
    h = 180,
    pad = 28;
  const pts = [...points].reverse();
  const max = Math.max(depth, ...pts.map((p) => p.pos ?? 0));
  const x = (i: number) => pad + (i * (w - 2 * pad)) / (pts.length - 1);
  const y = (p: number) => pad + ((p - 1) * (h - 2 * pad)) / Math.max(1, max - 1);
  let d = "";
  pts.forEach((p, i) => {
    if (p.pos === null) return;
    d += `${d && pts[i - 1]?.pos !== null ? "L" : "M"}${x(i).toFixed(1)},${y(p.pos).toFixed(1)} `;
  });
  return (
    <svg viewBox={`0 0 ${w} ${h}`} className="chart" role="img" aria-label="Position history">
      {[1, Math.ceil(max / 2), max].map((g) => (
        <g key={g}>
          <line x1={pad} x2={w - pad} y1={y(g)} y2={y(g)} className="grid" />
          <text x={4} y={y(g) + 4} className="axis">
            {g}
          </text>
        </g>
      ))}
      <path d={d} className="line" />
      {pts.map((p, i) =>
        p.pos === null ? (
          <text key={i} x={x(i)} y={h - 6} className="axis miss" textAnchor="middle">
            ×
          </text>
        ) : (
          <circle key={i} cx={x(i)} cy={y(p.pos)} r={3} className="dot">
            <title>{`${new Date(p.at).toLocaleDateString()}: ${p.pos}`}</title>
          </circle>
        ),
      )}
    </svg>
  );
}
