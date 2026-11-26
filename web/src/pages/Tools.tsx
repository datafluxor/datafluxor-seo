import { FormEvent, ReactNode, useState } from "react";
import { NavLink, useParams } from "react-router-dom";
import { api } from "../api";
import { useApp } from "../App";
import { MARKETS, languageFor } from "../countries";
import { Cost, DataTable, ErrorBox, firstRecords, KeyNotice, Scalars, Spinner, useLoad } from "../components/ui";

type ToolDef = {
  id: string;
  title: string;
  intro: string;
  cost: string;
  endpoint: string;
  fields: ("keyword" | "seed" | "keywords" | "target" | "brand" | "competitors" | "prompt" | "market" | "device" | "engine" | "view")[];
  build: (v: Record<string, string>) => Record<string, unknown>;
  // quote: ask DataFluxor for the price first (measure: false never charges)
  // and run the paid request only after the user agrees.
  quote?: boolean;
};

const TOOLS: ToolDef[] = [
  {
    id: "serp",
    title: "Google results",
    intro: "The live Google results page for any search, in any country: positions, AI Overview, featured snippet and People also ask.",
    cost: "5 credits",
    endpoint: "serp",
    fields: ["keyword", "market", "device"],
    build: (v) => ({ keyword: v.keyword, country: v.country, language: languageFor(v.country), device: v.device, depth: 10 }),
  },
  {
    id: "keywords",
    title: "Keyword research",
    intro: "Ideas around a seed keyword with search volume, difficulty and cost per click.",
    cost: "40+ credits",
    endpoint: "keyword-research",
    fields: ["seed", "market"],
    build: (v) => ({ seed: v.seed, country: v.country, language: languageFor(v.country), limit: 50, max_credits: 60 }),
    quote: true,
  },
  {
    id: "metrics",
    title: "Keyword metrics",
    intro: "Search volume, difficulty and cost per click for a list of keywords you already have.",
    cost: "varies, shown before charging",
    endpoint: "keyword-metrics",
    fields: ["keywords", "market"],
    build: (v) => ({
      keywords: v.keywords.split(/[\n,]/).map((k) => k.trim()).filter(Boolean).slice(0, 100),
      country: v.country,
      language: languageFor(v.country),
      max_credits: 50,
    }),
  },
  {
    id: "ai",
    title: "AI answers",
    intro: "Ask an AI assistant a question your customers ask, and see whether it names your brand and which competitors it names.",
    cost: "5 credits",
    endpoint: "ai-visibility",
    fields: ["prompt", "brand", "competitors", "engine", "market"],
    build: (v) => ({
      query: v.prompt,
      brand_name: v.brand,
      target_brand: v.brand,
      competitors: v.competitors.split(",").map((c) => c.trim()).filter(Boolean),
      engine: v.engine,
      country: v.country,
    }),
  },
  {
    id: "domain",
    title: "Domain overview",
    intro: "Estimated organic traffic, keywords and top pages of any website.",
    cost: "15-100 credits",
    endpoint: "domain-overview",
    fields: ["target", "market"],
    build: (v) => ({ target: v.target, country: v.country, language: languageFor(v.country) }),
    quote: true,
  },
  {
    id: "backlinks",
    title: "Backlinks",
    intro: "Who links to a website: referring domains, anchors and new or lost links.",
    cost: "free web graph, 60 credits for full backlink data",
    endpoint: "backlinks",
    fields: ["target", "view"],
    build: (v) => ({ target: v.target, view: v.view }),
    quote: true,
  },
];

export default function Tools() {
  const { tool } = useParams();
  const def = TOOLS.find((t) => t.id === tool) ?? TOOLS[0];
  return (
    <div className="page">
      <div className="tabs">
        {TOOLS.map((t) => (
          <NavLink key={t.id} to={`/tools/${t.id}`} className={({ isActive }) => (isActive || (t === def && !tool) ? "on" : "")}>
            {t.title}
          </NavLink>
        ))}
      </div>
      <ToolForm key={def.id} def={def} />
    </div>
  );
}

function ToolForm({ def }: { def: ToolDef }) {
  const { status } = useApp();
  const hasKey = !!status.has_api_key;
  const [v, setV] = useState<Record<string, string>>({ country: "US", device: "desktop", engine: "chatgpt_consumer", view: "web_graph", competitors: "" });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [res, setRes] = useState<{ data: unknown; credits_used: number; cache_hit: boolean } | null>(null);
  const engines = useLoad(
    () =>
      def.id === "ai" && hasKey
        ? api<{ data: { engines: { engine: string; label: string; available: boolean }[] } }>("/api/tools/ai-engines").then((r) => r.data.engines)
        : Promise.resolve([]),
    [def.id, hasKey],
  );
  const set = (k: string) => (e: { target: { value: string } }) => setV({ ...v, [k]: e.target.value });

  const [quote, setQuote] = useState<number | null>(null);
  const call = async (body: Record<string, unknown>) => {
    setBusy(true);
    setError(null);
    setRes(null);
    setQuote(null);
    try {
      const r = await api<{ data: any; credits_used: number; cache_hit: boolean }>(`/api/tools/${def.endpoint}`, { method: "POST", body });
      const price = r.data?.credits_to_measure;
      if (body.measure === false && typeof price === "number" && price > 0) setQuote(price);
      else setRes(r);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };
  const run = (e: FormEvent) => {
    e.preventDefault();
    const body = def.build(v);
    call(def.quote && body.view !== "web_graph" ? { ...body, measure: false } : body);
  };

  const field = (name: string, label: string, el: ReactNode) => (
    <label key={name}>
      {label}
      {el}
    </label>
  );
  const inputs: Record<string, ReactNode> = {
    keyword: field("keyword", "Search", <input value={v.keyword ?? ""} onChange={set("keyword")} required placeholder="best running shoes" />),
    seed: field("seed", "Seed keyword", <input value={v.seed ?? ""} onChange={set("seed")} required placeholder="running shoes" />),
    keywords: field("keywords", "Keywords (one per line)", <textarea rows={4} value={v.keywords ?? ""} onChange={set("keywords")} required />),
    target: field("target", "Website", <input value={v.target ?? ""} onChange={set("target")} required placeholder="example.com" />),
    prompt: field("prompt", "Question", <input value={v.prompt ?? ""} onChange={set("prompt")} required placeholder="What are the best running shoe brands?" />),
    brand: field("brand", "Your brand", <input value={v.brand ?? ""} onChange={set("brand")} required placeholder="Acme" />),
    competitors: field("competitors", "Competitors (comma separated)", <input value={v.competitors} onChange={set("competitors")} placeholder="Nike, Adidas" />),
    engine: field("engine", "Assistant", (
      <select value={v.engine} onChange={set("engine")}>
        {(engines.data?.length ? engines.data : [{ engine: "chatgpt_consumer", label: "ChatGPT", available: true }]).map((e) => (
          <option key={e.engine} value={e.engine} disabled={!e.available}>{e.label}{e.available ? "" : " (unavailable)"}</option>
        ))}
      </select>
    )),
    market: field("market", "Country", (
      <select value={v.country} onChange={set("country")}>
        {MARKETS.map(([c, , n]) => <option key={c} value={c}>{n}</option>)}
      </select>
    )),
    device: field("device", "Device", (
      <select value={v.device} onChange={set("device")}>
        <option value="desktop">Desktop</option>
        <option value="mobile">Mobile</option>
      </select>
    )),
    view: field("view", "View", (
      <select value={v.view} onChange={set("view")}>
        <option value="web_graph">Linking domains: Common Crawl web graph (free)</option>
        <option value="summary">Summary (60 credits)</option>
        <option value="referring_domains">Referring domains (60 credits)</option>
        <option value="anchors">Anchors (60 credits)</option>
        <option value="backlinks">Backlinks (60 credits)</option>
      </select>
    )),
  };

  return (
    <>
      <h1>{def.title}</h1>
      <p className="lead">{def.intro} <Cost text={def.cost} /></p>
      <KeyNotice hasKey={hasKey} />
      <form className="card stack" onSubmit={run}>
        <div className="row-form">{def.fields.map((f) => inputs[f])}</div>
        <button className="btn" disabled={!hasKey || busy}>{busy ? "Working…" : "Run"}</button>
      </form>
      <ErrorBox error={error} />
      {busy && <Spinner label=" Live checks can take up to a minute…" />}
      {quote !== null && (
        <div className="card">
          <p>
            This answer is not stored yet. Getting it costs up to <b>{quote} credits</b>.
          </p>
          <button className="btn" onClick={() => call(def.build(v))}>Get it for {quote} credits</button>{" "}
          <button className="btn btn-ghost" onClick={() => setQuote(null)}>Cancel</button>
        </div>
      )}
      {res && <Result def={def} res={res} />}
    </>
  );
}

function Result({ def, res }: { def: ToolDef; res: { data: unknown; credits_used: number; cache_hit: boolean } }) {
  const data = res.data as Record<string, any>;
  const recs = firstRecords(data);
  return (
    <div className="card">
      <p className="muted">
        {res.cache_hit ? "From cache" : "Live"} · {res.credits_used} credits used
      </p>
      {def.id === "serp" && Array.isArray(data?.items) ? (
        <>
          <div className="chips">
            {["has_ai_overview", "has_featured_snippet", "has_local_pack", "has_knowledge_panel"].map((k) =>
              data[k] ? <span key={k} className="badge notice">{k.replace("has_", "").replace(/_/g, " ")}</span> : null,
            )}
          </div>
          <ol className="serp">
            {data.items.map((r: any) => (
              <li key={r.position} value={r.position}>
                <a href={r.url} target="_blank" rel="noreferrer noopener">{r.title || r.url}</a>
                <div className="muted url">{r.url}</div>
                {r.snippet && <div>{r.snippet}</div>}
              </li>
            ))}
          </ol>
          {Array.isArray(data.people_also_ask) && data.people_also_ask.length > 0 && (
            <>
              <h3>People also ask</h3>
              <ul>{data.people_also_ask.map((q: any, i: number) => <li key={i}>{typeof q === "string" ? q : q.question}</li>)}</ul>
            </>
          )}
        </>
      ) : (
        <>
          {def.id === "ai" ? (
            <AIResult data={data} />
          ) : (
            <>
              <Scalars data={data} />
              {recs && <DataTable rows={recs.rows} />}
            </>
          )}
          <details>
            <summary className="muted">Raw answer</summary>
            <pre className="raw">{JSON.stringify(res.data, null, 2)}</pre>
          </details>
        </>
      )}
    </div>
  );
}

function AIResult({ data }: { data: Record<string, any> }) {
  if (!data) return null;
  const mentioned = data.brand_mentioned === true;
  const comps: any[] = Array.isArray(data.competitors) ? data.competitors : [];
  const cited: any[] = Array.isArray(data.cited_domains) ? data.cited_domains : [];
  return (
    <>
      <div className="grid">
        <div className="card stat">
          <div className="stat-label">{data.brand_name ?? "Your brand"}</div>
          <div className={`stat-big ${mentioned ? "good" : "bad"}`}>{mentioned ? "Mentioned" : "Not mentioned"}</div>
          <div className="muted">
            {data.brand_mention_count ?? 0} mentions · {data.brand_url_cited ? "your site is cited" : "your site is not cited"}
          </div>
        </div>
        {comps.map((c) => (
          <div key={c.competitor} className="card stat">
            <div className="stat-label">{c.competitor}</div>
            <div className={`stat-big ${c.mentioned ? "warn" : ""}`}>{c.mentioned ? "Mentioned" : "Not mentioned"}</div>
            <div className="muted">{c.mention_count ?? 0} mentions · {c.cited ? `cited ${c.citation_count}×` : "not cited"}</div>
          </div>
        ))}
      </div>
      {data.ai_generated_text ? (
        <>
          <h3>The answer</h3>
          <blockquote className="answer">{data.ai_generated_text}</blockquote>
        </>
      ) : (
        <p className="muted">No AI answer was shown for this question ({data.status}).</p>
      )}
      {cited.length > 0 && (
        <>
          <h3>Sites it cited</h3>
          <div className="chips">
            {cited.map((c) => (
              <span key={c.domain} className="chip">{c.domain}{c.count > 1 ? ` ×${c.count}` : ""}</span>
            ))}
          </div>
        </>
      )}
    </>
  );
}
