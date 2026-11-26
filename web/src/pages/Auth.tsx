import { FormEvent, useState } from "react";
import { api } from "../api";
import { ErrorBox } from "../components/ui";

function Card({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="auth">
      <div className="auth-card">
        <div className="auth-brand">
          <img src="/logo.svg" alt="" width={40} height={40} />
          <span>DataFluxor <b>SEO</b></span>
        </div>
        <h1>{title}</h1>
        {children}
      </div>
    </div>
  );
}

export function Setup({ onDone }: { onDone: () => void }) {
  const [code, setCode] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api("/api/setup", { method: "POST", body: { code, email, password } });
      onDone();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="Create your account">
      <p className="muted">
        This is a fresh install. Enter the <b>setup code</b> printed in the server log (<code>docker compose logs | grep "setup code"</code>), then
        choose the email and password you will sign in with.
      </p>
      <form onSubmit={submit} className="stack">
        <label>Setup code<input value={code} onChange={(e) => setCode(e.target.value)} placeholder="XXXX-XXXX-XXXX" required autoFocus /></label>
        <label>Email<input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoComplete="username" /></label>
        <label>Password (10+ characters)<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} minLength={10} required autoComplete="new-password" /></label>
        <ErrorBox error={error} />
        <button className="btn" disabled={busy}>{busy ? "Creating…" : "Create account"}</button>
      </form>
    </Card>
  );
}

export function Login({ onDone }: { onDone: () => void }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api("/api/login", { method: "POST", body: { email, password } });
      onDone();
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="Sign in">
      <form onSubmit={submit} className="stack">
        <label>Email<input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus autoComplete="username" /></label>
        <label>Password<input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="current-password" /></label>
        <ErrorBox error={error} />
        <button className="btn" disabled={busy}>{busy ? "Signing in…" : "Sign in"}</button>
      </form>
    </Card>
  );
}
