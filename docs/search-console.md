# Connect Google Search Console

DataFluxor SEO reads Search Console with your own Google sign-in client, so
your data goes straight from Google to your server. Setting it up takes about
five minutes and is free.

## 1. Create a Google Cloud project

1. Open <https://console.cloud.google.com/> and create a project (any name).
2. Go to **APIs & Services → Library**, search for **Google Search Console API** and click **Enable**.

## 2. Set up the consent screen

1. Go to **APIs & Services → OAuth consent screen** (called **Google Auth Platform** in newer consoles).
2. Choose **External**, give it a name such as "My SEO tool" and your email.
3. Under **Audience → Test users**, add the Google account that can see your sites in Search Console.

While the app stays in *testing*, only the test users you list can connect, which is what you want
for a private tool. Google may ask you to sign in again every 7 days in this mode; publishing the
app (no review is needed for your own use) removes that limit.

## 3. Create the client

1. Go to **APIs & Services → Credentials → Create credentials → OAuth client ID**.
2. Application type: **Web application**.
3. Under **Authorised redirect URIs**, add the address shown in the app's **Settings → Google Search Console**. It is your public address plus `/api/google/callback`, for example:
   - `http://localhost:8080/api/google/callback` on your own computer
   - `https://seo.example.com/api/google/callback` on a server
4. Click **Create** and copy the **Client ID** and **Client secret**.

## 4. Connect

1. In DataFluxor SEO, open **Settings → Google Search Console**, paste the client ID and secret and click **Save**
   (or set `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` in `.env`).
2. Click **Connect Google** and allow read-only access to Search Console.
3. Open a project's **Search Console** page and choose its property.

The app asks only for `webmasters.readonly`: it can read reports and never change anything in your account.
To disconnect, use **Settings → Disconnect**, and remove the app at <https://myaccount.google.com/permissions>.
