# Kalypt

Minimal Go and HTML5 website for Kalypt.

## Run

```sh
export SUPABASE_URL="https://your-project-ref.supabase.co"
export SUPABASE_SERVICE_ROLE_KEY="your-server-side-service-role-key"
export SUPABASE_INQUIRIES_TABLE="inquiries"
export INQUIRY_EMAIL_TO="estebanmarinramirez@icloud.com"
export RESEND_API_KEY="re_your-api-key"
export RESEND_FROM="Kalypt <inquiries@your-domain.com>"
go run .
```

Open `http://localhost:8080`.

## Render

Use a Go web service.

Build command:

```sh
go build -o app .
```

Start command:

```sh
./app
```

Render provides `PORT`; the server binds to it automatically.

Set these environment variables in Render:

```sh
SUPABASE_URL=https://your-project-ref.supabase.co
SUPABASE_SERVICE_ROLE_KEY=your-server-side-service-role-key
SUPABASE_INQUIRIES_TABLE=inquiries
INQUIRY_EMAIL_TO=estebanmarinramirez@icloud.com
RESEND_API_KEY=re_your-api-key
RESEND_FROM=Kalypt <inquiries@your-domain.com>
```

## Supabase

Run `supabase/schema.sql` in the Supabase SQL editor before enabling the contact form.

`SUPABASE_SERVICE_ROLE_KEY` must stay server-side. Do not expose it in browser JavaScript.

## Email Notifications

The contact endpoint saves each inquiry to Supabase, then sends a Resend email notification to `INQUIRY_EMAIL_TO`.

Create a Resend API key, verify the sender domain, and set `RESEND_FROM` to an address on that verified domain. Keep `RESEND_API_KEY` server-side in Render.

SMTP remains available as a local/legacy fallback if `RESEND_API_KEY` is not set, but Resend is preferred for Render because it uses HTTPS instead of outbound SMTP ports.
