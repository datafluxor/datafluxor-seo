import { FormEvent, useEffect, useMemo, useState } from "react";
import { api, fmtDate, Keyword, Project, RankCheck } from "../api";
import { useApp } from "../App";
import { MARKETS, languageFor } from "../countries";
import { Change, Cost, Empty, ErrorBox, KeyNotice, Position, PositionChart, Spinner, useLoad } from "../components/ui";

const FEATURES: Record<string, string> = {
  ai_overview: "AI Overview",
  featured_snippet: "Featured snippet",
  local_pack: "Local pack",
  knowledge_panel: "Knowledge panel",
  people_also_ask: "People also ask",
};

export default function Rankings({ project }: { project: Project }) {
  const { status } = useApp();
  const hasKey = !!status.has_api_key;
  const list = useLoad(() => api<Keyword[]>(`/api/projects/${project.id}/keywords`), [project.id]);
  const [text, setText] = useState("");
  const [country, setCountry] = useState("US");
  const [device, setDevice] = useState("desktop");
  const [error, setError] = useState<unknown>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState<number | "all" | null>(null);
  const [open, setOpen] = useState<number | null>(null);
  const [filter, setFilter] = useState("");
  const [sort, setSort] = useState<"keyword" | "position" | "change">("position");

  const add = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    const keywords = text.split(/[\n,]/).map((k) => k.trim()).filter(Boolean);
    if (!keywords.length) return;
    try {
      const r = await api<{ added: Keyword[]; already_tracked: number }>(`/api/projects/${project.id}/keywords`, {
        method: "POST",
        body: { keywords, country, language: languageFor(country), device },
      });
      setText("");
      setNotice(`${r.added.length} added${r.already_tracked ? `, ${r.already_tracked} already tracked` : ""}.${hasKey && r.added.length ? " Use Check to read their positions now, or wait for the daily check." : ""}`);
      list.reload();
    } catch (err) {
      setError(err);
    }
  };

  const check = async (k: Keyword) => {
    setBusy(k.id);
    setError(null);
    try {
      await api<RankCheck>(`/api/keywords/${k.id}/check`, { method: "POST" });
      list.reload();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(null);
    }
  };

  const checkAll = async () => {
    setError(null);
    try {
      const r = await api<{ queued: number }>(`/api/projects/${project.id}/check`, { method: "POST" });
      setNotice(`Checking ${r.queued} keywords in the background (about 7 seconds each). Refresh to see new positions.`);
      setBusy("all");
    } catch (err) {
      setError(err);
    }
  };

  useEffect(() => {
    if (busy !== "all") return;
    const t = setInterval(list.reload, 8000);
    const stop = setTimeout(() => setBusy(null), Math.max(30000, (list.data?.length ?? 0) * 9000));
    return () => {
      clearInterval(t);
      clearTimeout(stop);
    };
  }, [busy]);

  const remove = async (k: Keyword) => {
    if (!confirm(`Stop tracking "${k.keyword}" and delete its history?`)) return;
    await api(`/api/projects/${project.id}/keywords/${k.id}`, { method: "DELETE" }).catch(setError);
    list.reload();
  };

  const rows = useMemo(() => {
    const ks = (list.data ?? []).filter((k) => !filter || k.keyword.includes(filter.toLowerCase()));
    const pos = (k: Keyword) => k.latest?.position ?? 1000;
    const chg = (k: Keyword) => {
      const a = k.previous?.position ?? 101,
        b = k.latest?.position ?? 101;
      return k.previous ? a - b : 0;
    };
    return [...ks].sort((a, b) =>
      sort === "keyword" ? a.keyword.localeCompare(b.keyword) : sort === "position" ? pos(a) - pos(b) : chg(b) - chg(a),
    );
  }, [list.data, filter, sort]);

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>Rankings</h1>
          <p className="lead">
            Where {project.domain} appears in Google for each keyword, checked once a day. <Cost text="5 credits per check" />
          </p>
        </div>
        <div className="actions">
          <a className="btn btn-ghost" href={`/api/projects/${project.id}/keywords.csv`}>Export CSV</a>
          <button className="btn" onClick={checkAll} disabled={!hasKey || !list.data?.length || busy === "all"}>
            {busy === "all" ? "Checking…" : "Check all now"}
          </button>
        </div>
      </div>
      <KeyNotice hasKey={hasKey}>Rank checks read live Google results through the DataFluxor API.</KeyNotice>
      <form className="card add-keywords" onSubmit={add}>
        <label>
          Keywords (one per line)
          <textarea rows={3} value={text} onChange={(e) => setText(e.target.value)} placeholder={"running shoes\ntrail running shoes"} />
        </label>
        <div className="row-form">
          <label>
            Country
            <select value={country} onChange={(e) => setCountry(e.target.value)}>
              {MARKETS.map(([c, , name]) => (
                <option key={c} value={c}>{name}</option>
              ))}
            </select>
          </label>
          <label>
            Device
            <select value={device} onChange={(e) => setDevice(e.target.value)}>
              <option value="desktop">Desktop</option>
              <option value="mobile">Mobile</option>
            </select>
          </label>
          <button className="btn">Add keywords</button>
        </div>
      </form>
      {notice && <div className="alert alert-ok">{notice}</div>}
      <ErrorBox error={error ?? list.error} />
      {list.loading && !list.data ? (
        <Spinner />
      ) : !rows.length && !filter ? (
        <Empty>No keywords yet. Add the searches you want to rank for.</Empty>
      ) : (
        <>
          <div className="row-form">
            <input className="filter" placeholder="Filter keywords" value={filter} onChange={(e) => setFilter(e.target.value)} />
            <label className="inline">
              Sort
              <select value={sort} onChange={(e) => setSort(e.target.value as typeof sort)}>
                <option value="position">Position</option>
                <option value="change">Biggest gain</option>
                <option value="keyword">Keyword</option>
              </select>
            </label>
          </div>
          <div className="table-wrap">
            <table>
              <thead>
                <tr><th>Keyword</th><th>Market</th><th>Position</th><th>Change</th><th>Ranking page</th><th>On the page</th><th>Checked</th><th /></tr>
              </thead>
              <tbody>
                {rows.map((k) => (
                  <KeywordRow key={k.id} k={k} open={open === k.id} toggle={() => setOpen(open === k.id ? null : k.id)}
                    onCheck={() => check(k)} onRemove={() => remove(k)} busy={busy === k.id} hasKey={hasKey} />
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}

function KeywordRow({ k, open, toggle, onCheck, onRemove, busy, hasKey }: {
  k: Keyword; open: boolean; toggle: () => void; onCheck: () => void; onRemove: () => void; busy: boolean; hasKey: boolean;
}) {
  return (
    <>
      <tr className={open ? "open" : ""}>
        <td><button className="link" onClick={toggle}>{k.keyword}</button></td>
        <td className="muted">{k.country} · {k.device}</td>
        <td><Position value={k.latest ? k.latest.position : undefined} /></td>
        <td><Change prev={k.previous ? k.previous.position : undefined} cur={k.latest ? k.latest.position : undefined} /></td>
        <td>{k.latest?.url ? <a className="url" href={k.latest.url} target="_blank" rel="noreferrer noopener">{k.latest.url.replace(/^https?:\/\/[^/]+/, "") || "/"}</a> : null}</td>
        <td>{k.latest?.features.map((f) => <span key={f} className="badge notice" title={FEATURES[f]}>{FEATURES[f] ?? f}</span>)}</td>
        <td className="muted">{k.latest ? fmtDate(k.latest.checked_at) : "never"}</td>
        <td className="nowrap">
          <button className="btn btn-small" onClick={onCheck} disabled={!hasKey || busy}>{busy ? "…" : "Check"}</button>
          <button className="btn btn-small btn-ghost danger" onClick={onRemove} title="Stop tracking">×</button>
        </td>
      </tr>
      {open && (
        <tr className="detail">
          <td colSpan={8}><History k={k} /></td>
        </tr>
      )}
    </>
  );
}

function History({ k }: { k: Keyword }) {
  const { data, error, loading } = useLoad(
    () => api<{ history: RankCheck[]; latest_page?: RankCheck }>(`/api/keywords/${k.id}/history`),
    [k.id, k.latest?.id],
  );
  if (loading && !data) return <Spinner />;
  if (error) return <ErrorBox error={error} />;
  const hist = data?.history ?? [];
  const page = data?.latest_page;
  return (
    <div className="history-panel">
      <PositionChart points={hist.map((h) => ({ at: h.checked_at, pos: h.position }))} depth={hist[0]?.depth ?? 100} />
      {page?.results?.length ? (
        <>
          <h3>Google results on {fmtDate(page.checked_at)}</h3>
          <ol className="serp">
            {page.results.map((r) => (
              <li key={r.position} className={r.position === page.position ? "mine" : ""} value={r.position}>
                <a href={r.url} target="_blank" rel="noreferrer noopener">{r.title || r.url}</a>
                <div className="muted url">{r.url}</div>
              </li>
            ))}
          </ol>
        </>
      ) : (
        <p className="muted">No checks yet.</p>
      )}
    </div>
  );
}
