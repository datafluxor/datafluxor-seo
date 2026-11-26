import { useState } from "react";
import { Link } from "react-router-dom";
import { api, ApiError, fmtNum, GscRow, Project } from "../api";
import { useApp } from "../App";
import { Empty, ErrorBox, Spinner, useLoad } from "../components/ui";

type Report = { start: string; end: string; dimension: string; rows: GscRow[] };
const DIMENSIONS: [string, string][] = [
  ["query", "Searches"],
  ["page", "Pages"],
  ["country", "Countries"],
  ["device", "Devices"],
  ["date", "Days"],
];

export default function SearchConsole({ project }: { project: Project }) {
  const { status, reloadProjects } = useApp();
  const [dimension, setDimension] = useState("query");
  const [days, setDays] = useState(28);
  const settings = useLoad(() => api<{ google: { configured: boolean; connected: boolean } }>("/api/settings"), []);
  const google = settings.data?.google;
  const sites = useLoad(
    () => (google?.connected ? api<{ siteUrl: string; permissionLevel: string }[]>("/api/google/sites") : Promise.resolve([])),
    [google?.connected],
  );
  const end = new Date(Date.now() - 86400000);
  const start = new Date(end.getTime() - (days - 1) * 86400000);
  const iso = (d: Date) => d.toISOString().slice(0, 10);
  const report = useLoad(
    () =>
      google?.connected && project.gsc_property
        ? api<Report>(`/api/projects/${project.id}/search-console?dimension=${dimension}&start=${iso(start)}&end=${iso(end)}&limit=500`)
        : Promise.resolve(null),
    [google?.connected, project.gsc_property, dimension, days],
  );

  const choose = async (prop: string) => {
    await api(`/api/projects/${project.id}`, { method: "PATCH", body: { gsc_property: prop } });
    reloadProjects();
  };

  if (settings.loading) return <Spinner />;
  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>Search Console</h1>
          <p className="lead">Clicks, impressions and average position straight from Google, for free. Data stays on your server.</p>
        </div>
      </div>
      {!google?.configured ? (
        <div className="card">
          <p>
            To read Search Console, create a Google OAuth client (takes about five minutes) and paste its ID and secret in{" "}
            <Link to="/settings">Settings</Link>. Step-by-step guide:{" "}
            <a href="https://github.com/datafluxor/datafluxor-seo/blob/main/docs/search-console.md" target="_blank" rel="noreferrer">docs/search-console.md</a>.
          </p>
        </div>
      ) : !google.connected ? (
        <div className="card">
          <p>Connect the Google account that can see {project.domain} in Search Console. Access is read-only.</p>
          <a className="btn" href="/api/google/connect">Connect Google</a>
        </div>
      ) : (
        <>
          <div className="row-form">
            <label>
              Property
              <select value={project.gsc_property} onChange={(e) => choose(e.target.value)}>
                <option value="">Choose…</option>
                {(sites.data ?? []).map((s) => (
                  <option key={s.siteUrl} value={s.siteUrl}>{s.siteUrl}</option>
                ))}
              </select>
            </label>
            <label>
              Period
              <select value={days} onChange={(e) => setDays(Number(e.target.value))}>
                {[7, 28, 90, 180, 365, 480].map((d) => (
                  <option key={d} value={d}>Last {d} days</option>
                ))}
              </select>
            </label>
          </div>
          <ErrorBox error={sites.error} />
          {project.gsc_property && (
            <>
              <div className="tabs">
                {DIMENSIONS.map(([d, label]) => (
                  <button key={d} className={d === dimension ? "on" : ""} onClick={() => setDimension(d)}>{label}</button>
                ))}
              </div>
              {report.loading ? <Spinner /> : report.error ? <GscError error={report.error} /> : <GscTable report={report.data} />}
            </>
          )}
        </>
      )}
      {!status.has_api_key && (
        <p className="muted">Tip: with a DataFluxor key, the Rankings page tracks exact daily Google positions for the searches you find here.</p>
      )}
    </div>
  );
}

function GscError({ error }: { error: unknown }) {
  const e = error as ApiError;
  if (e.code === "GOOGLE_NOT_CONNECTED")
    return <div className="alert alert-error">Google access has expired. <a href="/api/google/connect">Connect again</a>.</div>;
  return <ErrorBox error={error} />;
}

function GscTable({ report }: { report: Report | null }) {
  const [sort, setSort] = useState<keyof GscRow>("clicks");
  if (!report) return null;
  if (!report.rows.length) return <Empty>Google has no data for this period yet.</Empty>;
  const rows = [...report.rows].sort((a, b) => (sort === "position" ? a.position - b.position : (b[sort] as number) - (a[sort] as number)));
  const total = report.rows.reduce((s, r) => ({ c: s.c + r.clicks, i: s.i + r.impressions }), { c: 0, i: 0 });
  const head = (k: keyof GscRow, label: string) => (
    <th><button className={`link ${sort === k ? "on" : ""}`} onClick={() => setSort(k)}>{label}</button></th>
  );
  return (
    <>
      <div className="grid">
        <div className="card stat"><div className="stat-label">Clicks</div><div className="stat-big">{fmtNum(total.c)}</div></div>
        <div className="card stat"><div className="stat-label">Impressions</div><div className="stat-big">{fmtNum(total.i)}</div></div>
        <div className="card stat"><div className="stat-label">CTR</div><div className="stat-big">{total.i ? fmtNum((total.c / total.i) * 100, 1) : 0}%</div></div>
      </div>
      <p className="muted">{report.start} to {report.end}. Totals are the sum of the rows shown.</p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr><th>{DIMENSIONS.find((d) => d[0] === report.dimension)?.[1]}</th>{head("clicks", "Clicks")}{head("impressions", "Impressions")}{head("ctr", "CTR")}{head("position", "Position")}</tr>
          </thead>
          <tbody>
            {rows.map((r) => (
              <tr key={r.keys.join("|")}>
                <td>{/^https?:/.test(r.keys[0]) ? <a className="url" href={r.keys[0]} target="_blank" rel="noreferrer noopener">{r.keys[0]}</a> : r.keys[0]}</td>
                <td>{fmtNum(r.clicks)}</td>
                <td>{fmtNum(r.impressions)}</td>
                <td>{fmtNum(r.ctr * 100, 1)}%</td>
                <td>{fmtNum(r.position, 1)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
