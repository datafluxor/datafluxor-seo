// Calls to this app's own server (/api). Writes carry the X-Requested-With
// header the server requires (it blocks cross-site requests).

export class ApiError extends Error {
  status: number;
  code?: string;
  signupUrl?: string;
  retryAfter?: number;
  constructor(status: number, message: string, code?: string, signupUrl?: string, retryAfter?: number) {
    super(message);
    this.status = status;
    this.code = code;
    this.signupUrl = signupUrl;
    this.retryAfter = retryAfter;
  }
}

export async function api<T = any>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") headers["X-Requested-With"] = "datafluxor-seo";
  if (init.body !== undefined) headers["Content-Type"] = "application/json";
  const res = await fetch(path, {
    method,
    headers,
    credentials: "same-origin",
    body: init.body === undefined ? undefined : JSON.stringify(init.body),
  });
  const text = await res.text();
  let data: any = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = null;
  }
  if (!res.ok) {
    throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`, data?.code, data?.signup_url, data?.retry_after_seconds);
  }
  return data as T;
}

export type User = { id: number; email: string };
export type Status = {
  setup_needed: boolean;
  signed_in: boolean;
  user?: User;
  version: string;
  has_api_key?: boolean;
  google_configured?: boolean;
  signup_url: string;
};
export type Project = { id: number; name: string; domain: string; gsc_property: string; created_at: string };
export type Counts = { errors: number; warnings: number; notices: number };
export type Audit = {
  id: number;
  project_id: number;
  start_url: string;
  status: "running" | "done" | "failed" | "stopped";
  max_pages: number;
  pages_crawled: number;
  error?: string;
  started_at: string;
  finished_at?: string;
  score?: number;
  counts: Counts;
};
export type AuditIssue = { page_url: string; code: string; severity: "error" | "warning" | "notice"; detail: string };
export type AuditPage = {
  url: string;
  status_code: number;
  title: string;
  meta_description: string;
  h1: string;
  canonical: string;
  noindex: boolean;
  words: number;
  response_ms: number;
  bytes: number;
  depth: number;
  redirect_to: string;
  inlinks: number;
};
export type RankResult = { position: number; url: string; domain: string; title: string };
export type RankCheck = {
  id: number;
  keyword_id: number;
  checked_at: string;
  position: number | null;
  url: string;
  depth: number;
  credits_used: number;
  cache_hit: boolean;
  results?: RankResult[];
  features: string[];
};
export type Keyword = {
  id: number;
  project_id: number;
  keyword: string;
  country: string;
  language: string;
  device: string;
  tags: string;
  latest?: RankCheck;
  previous?: RankCheck;
};
export type GscRow = { keys: string[]; clicks: number; impressions: number; ctr: number; position: number };

export const signupUrl = "https://datafluxor.com/signup?utm_source=github&utm_medium=self-hosted&utm_campaign=datafluxor-seo";

export function fmtDate(s?: string) {
  if (!s) return "";
  const d = new Date(s);
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function fmtNum(n: unknown, digits = 0) {
  if (typeof n !== "number" || !isFinite(n)) return "–";
  return n.toLocaleString(undefined, { maximumFractionDigits: digits });
}
