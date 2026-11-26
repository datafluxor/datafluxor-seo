import { FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, Audit, fmtDate, fmtNum, Project } from "../api";
import { useApp } from "../App";
import { ErrorBox, Spinner, useLoad } from "../components/ui";

type OverviewData = {
  project: Project;
  audits: Audit[];
  keywords: { tracked: number; checked: number; top3: number; top10: number; ranking: number; average_position: number | null; improved: number; declined: number };
  google_connected: boolean;
  has_api_key: boolean;
};

export default function Overview({ project }: { project: Project }) {
  const { reloadProjects } = useApp();
  const navigate = useNavigate();
  const { data, error } = useLoad(() => api<OverviewData>(`/api/projects/${project.id}/overview`), [project.id]);
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState(project.name);
  const [domain, setDomain] = useState(project.domain);
  const [saveErr, setSaveErr] = useState<unknown>(null);

  const save = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await api(`/api/projects/${project.id}`, { method: "PATCH", body: { name, domain } });
      reloadProjects();
      setEditing(false);
    } catch (err) {
      setSaveErr(err);
    }
  };
  const remove = async () => {
    if (!confirm(`Delete ${project.name} and all its audits and rankings? This cannot be undone.`)) return;
    await api(`/api/projects/${project.id}`, { method: "DELETE" });
    reloadProjects();
    navigate("/");
  };

  const last = data?.audits.find((a) => a.status === "done");
  const k = data?.keywords;
  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>{project.name}</h1>
          <a className="muted" href={`https://${project.domain}/`} target="_blank" rel="noreferrer">{project.domain}</a>
        </div>
        <div className="actions">
          <button className="btn btn-ghost" onClick={() => setEditing(!editing)}>Edit</button>
          <button className="btn btn-ghost danger" onClick={remove}>Delete</button>
        </div>
      </div>
      {editing && (
        <form className="card row-form" onSubmit={save}>
          <label>Name<input value={name} onChange={(e) => setName(e.target.value)} /></label>
          <label>Website<input value={domain} onChange={(e) => setDomain(e.target.value)} /></label>
          <button className="btn">Save</button>
          <ErrorBox error={saveErr} />
        </form>
      )}
      <ErrorBox error={error} />
      {!data ? (
        <Spinner label="Loading…" />
      ) : (
        <div className="grid">
          <Link to={`/projects/${project.id}/audit`} className="card stat">
            <div className="stat-label">Site health</div>
            {last ? (
              <>
                <div className={`stat-big ${scoreClass(last.score)}`}>{last.score ?? "–"}</div>
                <div className="muted">
                  {last.counts.errors} errors · {last.counts.warnings} warnings · {last.pages_crawled} pages · {fmtDate(last.finished_at)}
                </div>
              </>
            ) : (
              <div className="muted">No audit yet. Run a free audit →</div>
            )}
          </Link>
          <Link to={`/projects/${project.id}/rankings`} className="card stat">
            <div className="stat-label">Rankings</div>
            {k && k.tracked > 0 ? (
              <>
                <div className="stat-big">{k.average_position ? fmtNum(k.average_position, 1) : "–"}</div>
                <div className="muted">
                  average position · {k.top3} in top 3 · {k.top10} in top 10 of {k.tracked} keywords
                </div>
                <div>
                  <span className="up">▲ {k.improved}</span> <span className="down">▼ {k.declined}</span>
                </div>
              </>
            ) : (
              <div className="muted">{data.has_api_key ? "Add keywords to track →" : "Track Google positions with a DataFluxor key →"}</div>
            )}
          </Link>
          <Link to={`/projects/${project.id}/search-console`} className="card stat">
            <div className="stat-label">Search Console</div>
            <div className="muted">
              {data.google_connected
                ? project.gsc_property
                  ? `Connected: ${project.gsc_property}`
                  : "Google connected: choose this site's property →"
                : "Connect your Google account to see clicks and impressions (free) →"}
            </div>
          </Link>
        </div>
      )}
    </div>
  );
}

export function scoreClass(score?: number) {
  if (score === undefined) return "";
  return score >= 85 ? "good" : score >= 60 ? "warn" : "bad";
}
