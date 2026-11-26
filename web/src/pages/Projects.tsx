import { FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { api, fmtDate, Project } from "../api";
import { useApp } from "../App";
import { ErrorBox } from "../components/ui";

export default function Projects() {
  const { projects, reloadProjects } = useApp();
  const navigate = useNavigate();
  const [domain, setDomain] = useState("");
  const [name, setName] = useState("");
  const [error, setError] = useState<unknown>(null);
  const create = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const p = await api<Project>("/api/projects", { method: "POST", body: { domain, name } });
      reloadProjects();
      navigate(`/projects/${p.id}`);
    } catch (err) {
      setError(err);
    }
  };
  return (
    <div className="page">
      <h1>Projects</h1>
      <p className="lead">A project is one website. Audit it for free, track its Google rankings and connect its Search Console.</p>
      <form className="card row-form" onSubmit={create}>
        <label>
          Website
          <input value={domain} onChange={(e) => setDomain(e.target.value)} placeholder="example.com" required />
        </label>
        <label>
          Name (optional)
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="My shop" />
        </label>
        <button className="btn">Add project</button>
      </form>
      <ErrorBox error={error} />
      {projects.length > 0 && (
        <div className="grid">
          {projects.map((p) => (
            <Link key={p.id} to={`/projects/${p.id}`} className="card project-card">
              <span className="avatar" aria-hidden="true">{p.domain[0]?.toUpperCase()}</span>
              <div>
                <strong>{p.name}</strong>
                <div className="muted">{p.domain} · added {fmtDate(p.created_at)}</div>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
