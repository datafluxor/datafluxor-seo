import { useEffect, useMemo, useState } from "react";
import { api, Audit, AuditIssue, AuditPage, fmtDate, Project } from "../api";
import { Empty, ErrorBox, Spinner, useLoad } from "../components/ui";
import { scoreClass } from "./Overview";

// Plain-language names and fixes for each check.
const ISSUES: Record<string, [string, string]> = {
  fetch_failed: ["Page could not be read", "Check the server is up and the address works."],
  http_error: ["Page returns an error", "Fix or remove the page, or redirect it to a working one."],
  broken_link: ["Broken internal link", "Point the link at a page that works."],
  redirect_chain: ["Redirect chain", "Redirect straight to the final address."],
  linked_redirect: ["Links point at a redirect", "Update links to the final address."],
  missing_title: ["Missing title", "Add a unique <title> that says what the page is about."],
  long_title: ["Title too long", "Keep titles under about 60 characters."],
  short_title: ["Title too short", "Write a descriptive title of 30-60 characters."],
  multiple_titles: ["More than one title", "Keep a single <title> tag."],
  duplicate_title: ["Duplicate title", "Give every page its own title."],
  missing_description: ["Missing meta description", "Add a 120-160 character summary."],
  long_description: ["Meta description too long", "Keep it under about 160 characters."],
  short_description: ["Meta description too short", "Write 120-160 characters."],
  duplicate_description: ["Duplicate meta description", "Give every page its own description."],
  missing_h1: ["Missing H1", "Add one <h1> heading."],
  multiple_h1: ["Several H1 headings", "Use one <h1> and <h2>s below it."],
  thin_content: ["Thin content", "Add useful text, or noindex the page."],
  missing_lang: ["No language set", "Add lang=\"en\" (or yours) to <html>."],
  missing_viewport: ["No mobile viewport", "Add <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">."],
  image_alt: ["Images without alt text", "Describe each image in its alt attribute."],
  mixed_content: ["Mixed content", "Load every resource over https."],
  not_https: ["Page not on HTTPS", "Redirect http to https."],
  slow_page: ["Slow page", "Speed up the server or cache the page."],
  large_page: ["Very large HTML", "Trim the HTML; move data out of the page."],
  noindex: ["Page is noindex", "Fine if intended; otherwise remove noindex."],
  canonical_elsewhere: ["Canonical points elsewhere", "Fine for duplicates; otherwise point it at itself."],
  deep_page: ["Page is many clicks deep", "Link to it from higher-level pages."],
  missing_robots: ["No robots.txt", "Add a robots.txt with your sitemap."],
  missing_sitemap: ["No XML sitemap", "Publish a sitemap and list it in robots.txt."],
};

export default function AuditScreen({ project }: { project: Project }) {
  const list = useLoad(() => api<Audit[]>(`/api/projects/${project.id}/audits`), [project.id]);
  const [selected, setSelected] = useState<number | null>(null);
  const [maxPages, setMaxPages] = useState(500);
  const [error, setError] = useState<unknown>(null);
  const audits = list.data ?? [];
  const current = audits.find((a) => a.id === selected) ?? audits[0];
  const running = audits.some((a) => a.status === "running");

  useEffect(() => {
    if (!running) return;
    const t = setInterval(list.reload, 2000);
    return () => clearInterval(t);
  }, [running]);

  const start = async () => {
    setError(null);
    try {
      const a = await api<Audit>(`/api/projects/${project.id}/audits`, { method: "POST", body: { max_pages: maxPages } });
      setSelected(a.id);
      list.reload();
    } catch (e) {
      setError(e);
    }
  };
  const stop = async (id: number) => {
    await api(`/api/audits/${id}/stop`, { method: "POST" }).catch(setError);
    list.reload();
  };

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>Site audit</h1>
          <p className="lead">Crawls {project.domain} from your server and checks every page. Free: no API key needed.</p>
        </div>
        <div className="actions">
          <label className="inline">
            Pages
            <select value={maxPages} onChange={(e) => setMaxPages(Number(e.target.value))}>
              {[100, 500, 1000, 2500, 5000].map((n) => (
                <option key={n} value={n}>up to {n}</option>
              ))}
            </select>
          </label>
          <button className="btn" onClick={start} disabled={running}>{running ? "Audit running…" : "Run audit"}</button>
        </div>
      </div>
      <ErrorBox error={error ?? list.error} />
      {list.loading && !list.data ? (
        <Spinner />
      ) : !current ? (
        <Empty>No audits yet. Run your first one above.</Empty>
      ) : (
        <>
          {audits.length > 1 && (
            <div className="history">
              {audits.slice(0, 12).map((a) => (
                <button key={a.id} className={`chip ${a.id === current.id ? "on" : ""}`} onClick={() => setSelected(a.id)}>
                  {fmtDate(a.started_at)} {a.score !== undefined ? `· ${a.score}` : `· ${a.status}`}
                </button>
              ))}
            </div>
          )}
          <AuditView audit={current} previous={audits.find((a) => a.id < current.id && a.status === "done")} onStop={stop} />
        </>
      )}
    </div>
  );
}

function AuditView({ audit, previous, onStop }: { audit: Audit; previous?: Audit; onStop: (id: number) => void }) {
  const [tab, setTab] = useState<"issues" | "pages">("issues");
  const done = audit.status !== "running";
  const issues = useLoad(() => (done ? api<AuditIssue[]>(`/api/audits/${audit.id}/issues`) : Promise.resolve([])), [audit.id, done]);
  const pages = useLoad(() => (done && tab === "pages" ? api<AuditPage[]>(`/api/audits/${audit.id}/pages`) : Promise.resolve([])), [audit.id, done, tab]);

  if (audit.status === "running") {
    return (
      <div className="card">
        <Spinner label={` Crawling ${audit.start_url}: ${audit.pages_crawled} of up to ${audit.max_pages} pages…`} />
        <div className="progress"><div style={{ width: `${Math.min(100, (audit.pages_crawled / audit.max_pages) * 100)}%` }} /></div>
        <button className="btn btn-ghost" onClick={() => onStop(audit.id)}>Stop</button>
      </div>
    );
  }
  const delta = (cur: number, prev?: number) =>
    prev === undefined ? null : cur === prev ? null : <span className={cur < prev ? "up" : "down"}>{cur < prev ? `▼${prev - cur}` : `▲${cur - prev}`}</span>;
  return (
    <>
      {audit.status !== "done" && <div className="alert alert-error">Audit {audit.status}: {audit.error}</div>}
      <div className="grid">
        <div className="card stat">
          <div className="stat-label">Health score</div>
          <div className={`stat-big ${scoreClass(audit.score)}`}>{audit.score ?? "–"}</div>
          <div className="muted">{audit.pages_crawled} pages · {fmtDate(audit.finished_at)}</div>
        </div>
        <div className="card stat">
          <div className="stat-label">Errors</div>
          <div className="stat-big bad">{audit.counts.errors} {delta(audit.counts.errors, previous?.counts.errors)}</div>
        </div>
        <div className="card stat">
          <div className="stat-label">Warnings</div>
          <div className="stat-big warn">{audit.counts.warnings} {delta(audit.counts.warnings, previous?.counts.warnings)}</div>
        </div>
        <div className="card stat">
          <div className="stat-label">Notices</div>
          <div className="stat-big">{audit.counts.notices}</div>
        </div>
      </div>
      <div className="tabs">
        <button className={tab === "issues" ? "on" : ""} onClick={() => setTab("issues")}>Issues</button>
        <button className={tab === "pages" ? "on" : ""} onClick={() => setTab("pages")}>Pages</button>
        <a className="btn btn-ghost" href={`/api/audits/${audit.id}/issues.csv`}>Export CSV</a>
      </div>
      {tab === "issues" ? <IssueGroups issues={issues.data ?? []} loading={issues.loading} /> : <PagesTable pages={pages.data ?? []} loading={pages.loading} />}
    </>
  );
}

function IssueGroups({ issues, loading }: { issues: AuditIssue[]; loading: boolean }) {
  const groups = useMemo(() => {
    const m = new Map<string, AuditIssue[]>();
    for (const i of issues) m.set(i.code, [...(m.get(i.code) ?? []), i]);
    return [...m.entries()];
  }, [issues]);
  if (loading) return <Spinner />;
  if (!groups.length) return <Empty>No issues found. Nice work.</Empty>;
  return (
    <div className="issues">
      {groups.map(([code, list]) => (
        <details key={code} className={`issue sev-${list[0].severity}`}>
          <summary>
            <span className={`badge ${list[0].severity}`}>{list[0].severity}</span>
            <strong>{ISSUES[code]?.[0] ?? code}</strong>
            <span className="muted">{list.length} page{list.length > 1 ? "s" : ""}</span>
          </summary>
          {ISSUES[code] && <p className="fix">How to fix: {ISSUES[code][1]}</p>}
          <ul>
            {list.slice(0, 200).map((i, n) => (
              <li key={n}>
                <a href={i.page_url} target="_blank" rel="noreferrer noopener" className="url">{i.page_url}</a>
                <span className="muted"> {i.detail}</span>
              </li>
            ))}
          </ul>
          {list.length > 200 && <p className="muted">…and {list.length - 200} more (see the CSV).</p>}
        </details>
      ))}
    </div>
  );
}

function PagesTable({ pages, loading }: { pages: AuditPage[]; loading: boolean }) {
  const [q, setQ] = useState("");
  if (loading) return <Spinner />;
  const shown = pages.filter((p) => !q || p.url.includes(q) || p.title.toLowerCase().includes(q.toLowerCase()));
  return (
    <>
      <input className="filter" placeholder="Filter by address or title" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="table-wrap">
        <table>
          <thead>
            <tr><th>Address</th><th>Status</th><th>Title</th><th>Words</th><th>Time</th><th>Depth</th><th>Links in</th></tr>
          </thead>
          <tbody>
            {shown.slice(0, 1000).map((p) => (
              <tr key={p.url}>
                <td><a href={p.url} target="_blank" rel="noreferrer noopener" className="url">{p.url.replace(/^https?:\/\/[^/]+/, "") || "/"}</a></td>
                <td className={p.status_code >= 400 || p.status_code === 0 ? "bad" : p.status_code >= 300 ? "warn" : ""}>{p.status_code || "failed"}{p.redirect_to ? " →" : ""}</td>
                <td>{p.title || <span className="muted">–</span>}{p.noindex && <span className="badge notice">noindex</span>}</td>
                <td>{p.words || ""}</td>
                <td>{p.response_ms} ms</td>
                <td>{p.depth}</td>
                <td>{p.inlinks}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
