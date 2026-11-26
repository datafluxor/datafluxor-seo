<p align="center">
  <a href="https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo"><strong>DataFluxor SEO</strong></a><br>
  Open-source, self-hosted SEO toolkit: site audits, rank tracking, Search Console, keyword research and AI-answer tracking.
</p>

<p align="center">
  <a href="https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo">Website</a> ·
  <a href="https://datafluxor.com/signup?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo">Get a free API key</a> ·
  <a href="#quick-start">Self-host in 2 minutes</a>
</p>

> **Status: early release ([v0.1.0](https://github.com/datafluxor/datafluxor-seo/releases/latest)).** Works end to end; expect rough edges. Prefer no set-up? Use the hosted version at **[datafluxor.com](https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo)**.

## Two ways to use it

| | **DataFluxor Cloud** | **Self-hosted (this repo)** |
|---|---|---|
| Set-up | None: [sign up](https://datafluxor.com/signup?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo) | One Docker command on your own server |
| Site audit, Search Console | ✓ | ✓ free, no key needed |
| Live Google rankings, keyword data, AI answers, backlinks | ✓ | ✓ with a DataFluxor API key |
| Your data lives | On DataFluxor | On your server (one SQLite file) |

## Quick start

```bash
git clone https://github.com/datafluxor/datafluxor-seo.git
cd datafluxor-seo
docker compose up -d
docker compose logs | grep "setup code"
```

1. Open **http://localhost:8080** and create your account with the **setup code** from the log.
2. Add a project (your website) and run a **site audit**: free, no key.
3. For live Google rankings, keyword research and AI-answer tracking, paste a **DataFluxor API key** in **Settings**. [Get one free](https://datafluxor.com/signup?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo): new accounts include starter credits.

## What it does

**Free, no API key**
- **Site audit:** crawls your site from your server and finds broken links, server errors, redirect chains, missing or duplicate titles and descriptions, missing H1s, thin pages, images without alt text, mixed content, slow pages, noindex and canonical problems, missing sitemap or robots.txt. Health score, history and CSV export.
- **Search Console:** connect your own Google account to see clicks, impressions, CTR and average position by query, page, country and device.

**With a DataFluxor API key**
- **Rank tracking:** daily Google positions for your keywords by country and device, position history chart, winners and losers, the full results page for each check, SERP features (AI Overview, featured snippet, local pack) and CSV export.
- **Google results:** the live top results for any search, with People Also Ask and AI Overview.
- **Keyword research and metrics:** search volume, CPC, difficulty and keyword ideas.
- **AI answers:** whether ChatGPT, Perplexity, Gemini and Google AI Overviews mention your brand and your competitors, and which sites they cite.
- **Domain overview and backlinks** (the Common Crawl linking-domains view is free).

### What it costs

The app is free. Data calls use DataFluxor credits from your account:

| Action | Credits |
|---|---|
| Rank check or Google results page | 5 (a page fetched in the last 12 hours is reused free) |
| AI answer check | 5 |
| Keyword metrics | about 1 per keyword |
| Keyword research | from 40 per search |
| Domain overview | 15-100 |
| Backlinks | 60 (free Common Crawl view: 0) |

Expensive tools show the price and ask before charging. Settings has a monthly credit limit for the daily rank checks.

## Running it on a server

`docker-compose.yml` publishes the app on `127.0.0.1:8080` only. Put it behind a web server that adds HTTPS, for example Caddy:

```
seo.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

and set `DFSEO_PUBLIC_URL=https://seo.example.com` in `.env` (copy `.env.example`). With an `https` address the sign-in cookie is sent over HTTPS only.

Your data is one SQLite file in the `dfseo-data` volume. Back it up with `docker compose cp datafluxor-seo:/data ./backup`.

**Search Console:** see [docs/search-console.md](docs/search-console.md) to create the Google sign-in client (about five minutes).

### Without Docker

Download the archive for Linux, macOS or Windows from the [latest release](https://github.com/datafluxor/datafluxor-seo/releases/latest), unpack it and run `./datafluxor-seo`. It prints the setup code; open http://localhost:8080.

To build from source (Go 1.26+ and Node 22+):

```bash
make            # builds the web app and bin/datafluxor-seo
./bin/datafluxor-seo
```

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `DFSEO_PUBLIC_URL` | `http://localhost:8080` | The address you open the app at (used for Google sign-in) |
| `DFSEO_DATA_DIR` | `./data` | Where the database is kept |
| `DATAFLUXOR_API_KEY` | – | Optional: set the API key here instead of in Settings |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | – | Optional: your Google OAuth client for Search Console |
| `DFSEO_ADDR` | `:8080` | Listen address |
| `DFSEO_CRAWL_MAX_PAGES` | `5000` | Largest site audit a user can start |
| `DFSEO_CRAWL_ALLOW_PRIVATE` | `false` | Allow audits of private network addresses (intranet sites) |

## Development

```bash
go test ./...                 # server tests
cd web && npm ci && npm run dev   # web app with hot reload, proxied to a server on :8080
```

Issues and pull requests are welcome: see [CONTRIBUTING.md](CONTRIBUTING.md). Report security problems privately: [SECURITY.md](SECURITY.md).

## Licence

[AGPL-3.0](LICENSE). "DataFluxor" is a trademark: see [TRADEMARK.md](TRADEMARK.md).
