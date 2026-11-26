<p align="center">
  <a href="https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo"><strong>DataFluxor SEO</strong></a><br>
  Open-source, self-hosted SEO toolkit: site audits, rank tracking, Search Console, keyword research and AI-answer tracking.
</p>

<p align="center">
  <a href="https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo">Website</a> ·
  <a href="https://datafluxor.com/signup?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo">Get a free API key</a> ·
  <a href="#quick-start">Self-host in 2 minutes</a>
</p>

> **Status: first release in progress.** The code is being published here now; until the first tagged release, use the hosted version at **[datafluxor.com](https://datafluxor.com/?utm_source=github&utm_medium=readme&utm_campaign=datafluxor-seo)**.

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
- **Rank tracking:** daily Google positions for your keywords by country and device, history, movers, SERP features (AI Overview, featured snippet, local pack) and a cross-check against Search Console.
- **Google results:** the live top results for any search, with People Also Ask and AI Overview.
- **Keyword research:** search volume, CPC, difficulty and keyword ideas.
- **AI answers:** whether ChatGPT, Perplexity, Gemini and Google AI Overviews mention your brand.
- **Domain overview and backlinks.**

Every paid call shows its credit cost, and you can set a monthly credit cap.

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `DFSEO_PUBLIC_URL` | `http://localhost:8080` | The address you open the app at (used for Google sign-in) |
| `DFSEO_DATA_DIR` | `./data` | Where the database is kept |
| `DATAFLUXOR_API_KEY` | – | Optional: set the API key here instead of in Settings |
| `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` | – | Optional: your Google OAuth client for Search Console |

## Licence

[AGPL-3.0](LICENSE). "DataFluxor" is a trademark: see [TRADEMARK.md](TRADEMARK.md).
