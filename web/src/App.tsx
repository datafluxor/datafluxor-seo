import { createContext, useContext, useEffect, useState } from "react";
import { NavLink, Navigate, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { api, Project, signupUrl, Status } from "./api";
import { Spinner } from "./components/ui";
import { Login, Setup } from "./pages/Auth";
import Projects from "./pages/Projects";
import Overview from "./pages/Overview";
import AuditPage from "./pages/Audit";
import Rankings from "./pages/Rankings";
import SearchConsole from "./pages/SearchConsole";
import Tools from "./pages/Tools";
import Settings from "./pages/Settings";

type AppState = { status: Status; refresh: () => void; projects: Project[]; reloadProjects: () => void };
const Ctx = createContext<AppState | null>(null);
export const useApp = () => useContext(Ctx)!;

export default function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [projects, setProjects] = useState<Project[]>([]);
  const [failed, setFailed] = useState("");

  const refresh = () =>
    api<Status>("/api/status")
      .then(setStatus)
      .catch((e) => setFailed(e.message));
  const reloadProjects = () => api<Project[]>("/api/projects").then(setProjects).catch(() => undefined);

  useEffect(() => {
    refresh();
  }, []);
  useEffect(() => {
    if (status?.signed_in) reloadProjects();
  }, [status?.signed_in]);

  if (failed) return <div className="center">Cannot reach the server: {failed}</div>;
  if (!status) return <div className="center"><Spinner label="Loading…" /></div>;
  if (status.setup_needed) return <Setup onDone={refresh} />;
  if (!status.signed_in) return <Login onDone={refresh} />;

  return (
    <Ctx.Provider value={{ status, refresh, projects, reloadProjects }}>
      <div className="shell">
        <Sidebar />
        <main className="main">
          {!status.has_api_key && (
            <div className="banner">
              Site audits and Search Console are free. For live Google rankings, keyword data and AI answers,{" "}
              <a href={signupUrl} target="_blank" rel="noreferrer">get a free DataFluxor API key</a> and add it in{" "}
              <NavLink to="/settings">Settings</NavLink>.
            </div>
          )}
          <Routes>
            <Route path="/" element={<Projects />} />
            <Route path="/projects/:id" element={<ProjectRoute view="overview" />} />
            <Route path="/projects/:id/audit" element={<ProjectRoute view="audit" />} />
            <Route path="/projects/:id/rankings" element={<ProjectRoute view="rankings" />} />
            <Route path="/projects/:id/search-console" element={<ProjectRoute view="gsc" />} />
            <Route path="/tools/:tool?" element={<Tools />} />
            <Route path="/settings" element={<Settings />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
          <footer className="foot">
            DataFluxor SEO {status.version} · <a href="https://github.com/datafluxor/datafluxor-seo" target="_blank" rel="noreferrer">GitHub</a> ·{" "}
            <a href="https://datafluxor.com/?utm_source=github&utm_medium=self-hosted&utm_campaign=datafluxor-seo" target="_blank" rel="noreferrer">datafluxor.com</a>
          </footer>
        </main>
      </div>
    </Ctx.Provider>
  );
}

function ProjectRoute({ view }: { view: "overview" | "audit" | "rankings" | "gsc" }) {
  const { id } = useParams();
  const { projects } = useApp();
  const project = projects.find((p) => String(p.id) === id);
  if (!project) return projects.length ? <Navigate to="/" replace /> : <Spinner label="Loading…" />;
  switch (view) {
    case "audit":
      return <AuditPage project={project} />;
    case "rankings":
      return <Rankings project={project} />;
    case "gsc":
      return <SearchConsole project={project} />;
    default:
      return <Overview project={project} />;
  }
}

function Sidebar() {
  const { projects, status, refresh } = useApp();
  const navigate = useNavigate();
  const logout = async () => {
    await api("/api/logout", { method: "POST" });
    refresh();
    navigate("/");
  };
  return (
    <nav className="side">
      <NavLink to="/" className="brand">
        <img src="/logo.svg" alt="" width={28} height={28} />
        <span>DataFluxor <b>SEO</b></span>
      </NavLink>
      <div className="side-group">
        <div className="side-title">Projects</div>
        {projects.map((p) => (
          <details key={p.id} open={location.pathname.startsWith(`/projects/${p.id}`)} className="side-project">
            <summary>{p.name}</summary>
            <NavLink end to={`/projects/${p.id}`}>Overview</NavLink>
            <NavLink to={`/projects/${p.id}/audit`}>Site audit</NavLink>
            <NavLink to={`/projects/${p.id}/rankings`}>Rankings</NavLink>
            <NavLink to={`/projects/${p.id}/search-console`}>Search Console</NavLink>
          </details>
        ))}
        <NavLink end to="/" className="side-add">+ New project</NavLink>
      </div>
      <div className="side-group">
        <div className="side-title">Research</div>
        <NavLink to="/tools/serp">Google results</NavLink>
        <NavLink to="/tools/keywords">Keyword research</NavLink>
        <NavLink to="/tools/ai">AI answers</NavLink>
        <NavLink to="/tools/domain">Domain overview</NavLink>
        <NavLink to="/tools/backlinks">Backlinks</NavLink>
      </div>
      <div className="side-bottom">
        <NavLink to="/settings">Settings</NavLink>
        <button className="link" onClick={logout}>Sign out ({status.user?.email})</button>
      </div>
    </nav>
  );
}
