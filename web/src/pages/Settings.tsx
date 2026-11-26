import { FormEvent, useEffect, useState } from "react";
import { api, fmtDate, signupUrl } from "../api";
import { useApp } from "../App";
import { ErrorBox, Scalars, Spinner, useLoad } from "../components/ui";

type SettingsData = {
  api_key_set: boolean;
  api_key_from_env: boolean;
  api_key_suffix: string;
  api_url: string;
  rank_depth: number;
  rank_depths: number[];
  rank_check_hour: number;
  monthly_credit_cap: number;
  rank_schedule_paused: boolean;
  public_url: string;
  google: { configured: boolean; from_env: boolean; client_id: string; connected: boolean; redirect_url: string };
};
type Account = {
  spent_this_month: number;
  recent_spend: { at: string; tool: string; credits: number; note: string }[];
  has_api_key: boolean;
  account?: Record<string, unknown>;
  account_error?: string;
};

export default function Settings() {
  const { refresh } = useApp();
  const s = useLoad(() => api<SettingsData>("/api/settings"), []);
  const acct = useLoad(() => api<Account>("/api/account"), [s.data?.api_key_set]);
  const [msg, setMsg] = useState("");
  const [error, setError] = useState<unknown>(null);
  const googleResult = new URLSearchParams(location.search).get("google");

  const save = async (body: Record<string, unknown>, done: string) => {
    setError(null);
    setMsg("");
    try {
      s.setData(await api<SettingsData>("/api/settings", { method: "PUT", body }));
      setMsg(done);
      refresh();
    } catch (e) {
      setError(e);
    }
  };

  if (s.loading && !s.data) return <Spinner />;
  const d = s.data;
  if (!d) return <ErrorBox error={s.error} />;
  return (
    <div className="page narrow">
      <h1>Settings</h1>
      {msg && <div className="alert alert-ok">{msg}</div>}
      {googleResult && (
        <div className={`alert ${googleResult === "connected" ? "alert-ok" : "alert-error"}`}>
          {googleResult === "connected" ? "Google connected." : `Google sign-in did not finish (${googleResult}).`}
        </div>
      )}
      <ErrorBox error={error} />

      <section className="card">
        <h2>DataFluxor API key</h2>
        <p className="muted">
          Rank tracking, Google results, keyword data, AI answers and backlinks use the DataFluxor API. Site audits and Search Console work without a key.
        </p>
        {d.api_key_from_env ? (
          <p>The key is set by the <code>DATAFLUXOR_API_KEY</code> environment variable (ends in …{d.api_key_suffix}).</p>
        ) : (
          <KeyForm d={d} onSave={save} />
        )}
        {!d.api_key_set && (
          <p>
            No key yet? <a href={signupUrl} target="_blank" rel="noreferrer">Create a free DataFluxor account</a>, open API keys and copy one here.
          </p>
        )}
      </section>

      <section className="card">
        <h2>Credits</h2>
        {acct.loading && !acct.data ? <Spinner /> : acct.data && (
          <>
            <p>
              Used by this app this month: <b>{acct.data.spent_this_month}</b> credits.{" "}
              <a href="https://datafluxor.com/app/billing?utm_source=github&utm_medium=self-hosted&utm_campaign=datafluxor-seo" target="_blank" rel="noreferrer">Buy credits</a>
            </p>
            {acct.data.account_error && <div className="alert alert-error">{acct.data.account_error}</div>}
            {acct.data.account && <Scalars data={(acct.data.account as any).credits ?? acct.data.account} />}
            {acct.data.recent_spend?.length > 0 && (
              <details>
                <summary>Recent charges</summary>
                <table>
                  <tbody>
                    {acct.data.recent_spend.map((r, i) => (
                      <tr key={i}><td className="muted">{fmtDate(r.at)}</td><td>{r.tool}</td><td>{r.note}</td><td>{r.credits}</td></tr>
                    ))}
                  </tbody>
                </table>
              </details>
            )}
          </>
        )}
      </section>

      <section className="card">
        <h2>Daily rank checks</h2>
        <ScheduleForm d={d} onSave={save} />
      </section>

      <section className="card">
        <h2>Google Search Console</h2>
        <GoogleForm d={d} onSave={save} reload={s.reload} />
      </section>

      <section className="card">
        <h2>Password</h2>
        <PasswordForm />
      </section>
    </div>
  );
}

type FormProps = { d: SettingsData; onSave: (b: Record<string, unknown>, done: string) => Promise<void> };

function KeyForm({ d, onSave }: FormProps) {
  const [key, setKey] = useState("");
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    await onSave({ api_key: key.trim() }, "API key saved and checked.");
    setKey("");
  };
  return (
    <form className="row-form" onSubmit={submit}>
      <label>
        {d.api_key_set ? `Key saved (ends in …${d.api_key_suffix}). Replace it:` : "Paste your key"}
        <input type="password" value={key} onChange={(e) => setKey(e.target.value)} placeholder="df_live_…" autoComplete="off" required />
      </label>
      <button className="btn">Save key</button>
      {d.api_key_set && (
        <button type="button" className="btn btn-ghost danger" onClick={() => confirm("Remove the API key?") && onSave({ api_key: "" }, "API key removed.")}>
          Remove
        </button>
      )}
    </form>
  );
}

function ScheduleForm({ d, onSave }: FormProps) {
  const [depth, setDepth] = useState(d.rank_depth);
  const [hour, setHour] = useState(d.rank_check_hour);
  const [cap, setCap] = useState(d.monthly_credit_cap);
  const [paused, setPaused] = useState(d.rank_schedule_paused);
  useEffect(() => {
    setDepth(d.rank_depth);
    setHour(d.rank_check_hour);
    setCap(d.monthly_credit_cap);
    setPaused(d.rank_schedule_paused);
  }, [d]);
  return (
    <form
      className="stack"
      onSubmit={(e) => {
        e.preventDefault();
        onSave({ rank_depth: depth, rank_check_hour: hour, monthly_credit_cap: cap, rank_schedule_paused: paused }, "Schedule saved.");
      }}
    >
      <div className="row-form">
        <label>
          Results to read
          <select value={depth} onChange={(e) => setDepth(Number(e.target.value))}>
            {d.rank_depths.map((n) => <option key={n} value={n}>Top {n}</option>)}
          </select>
        </label>
        <label>
          Check time (UTC)
          <select value={hour} onChange={(e) => setHour(Number(e.target.value))}>
            {Array.from({ length: 24 }, (_, h) => <option key={h} value={h}>{String(h).padStart(2, "0")}:00</option>)}
          </select>
        </label>
        <label>
          Monthly credit limit (0 = none)
          <input type="number" min={0} value={cap} onChange={(e) => setCap(Number(e.target.value))} />
        </label>
      </div>
      <label className="check">
        <input type="checkbox" checked={paused} onChange={(e) => setPaused(e.target.checked)} /> Pause daily checks
      </label>
      <p className="muted">Each keyword check costs 5 credits; a page Google served in the last 12 hours may be reused at no extra charge.</p>
      <button className="btn">Save schedule</button>
    </form>
  );
}

function GoogleForm({ d, onSave, reload }: FormProps & { reload: () => void }) {
  const [id, setId] = useState(d.google.client_id);
  const [secret, setSecret] = useState("");
  const disconnect = async () => {
    await api("/api/google/disconnect", { method: "POST" });
    reload();
  };
  return (
    <div className="stack">
      <p className="muted">
        Create an OAuth client of type <b>Web application</b> in Google Cloud and add this redirect address: <code>{d.google.redirect_url}</code>.{" "}
        <a href="https://github.com/datafluxor/datafluxor-seo/blob/main/docs/search-console.md" target="_blank" rel="noreferrer">Step-by-step guide</a>
      </p>
      {d.google.from_env ? (
        <p>The client is set by <code>GOOGLE_CLIENT_ID</code> and <code>GOOGLE_CLIENT_SECRET</code>.</p>
      ) : (
        <form className="row-form" onSubmit={(e) => { e.preventDefault(); onSave({ google_client_id: id, google_client_secret: secret }, "Google client saved."); setSecret(""); }}>
          <label>Client ID<input value={id} onChange={(e) => setId(e.target.value)} /></label>
          <label>Client secret<input type="password" value={secret} onChange={(e) => setSecret(e.target.value)} placeholder={d.google.configured ? "saved" : ""} autoComplete="off" /></label>
          <button className="btn">Save</button>
        </form>
      )}
      {d.google.configured && (d.google.connected ? (
        <p>Google account connected. <button className="link danger" onClick={disconnect}>Disconnect</button></p>
      ) : (
        <a className="btn" href="/api/google/connect">Connect Google</a>
      ))}
    </div>
  );
}

function PasswordForm() {
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  const [msg, setMsg] = useState("");
  const [error, setError] = useState<unknown>(null);
  return (
    <form
      className="row-form"
      onSubmit={async (e) => {
        e.preventDefault();
        setError(null);
        try {
          await api("/api/password", { method: "POST", body: { current: cur, new: next } });
          setMsg("Password changed. Other sessions were signed out.");
          setCur("");
          setNext("");
        } catch (err) {
          setError(err);
        }
      }}
    >
      <label>Current<input type="password" value={cur} onChange={(e) => setCur(e.target.value)} required autoComplete="current-password" /></label>
      <label>New (10+ characters)<input type="password" value={next} onChange={(e) => setNext(e.target.value)} minLength={10} required autoComplete="new-password" /></label>
      <button className="btn">Change password</button>
      {msg && <span className="ok">{msg}</span>}
      <ErrorBox error={error} />
    </form>
  );
}
