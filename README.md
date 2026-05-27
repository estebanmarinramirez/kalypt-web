# Kalypt

Minimal Go and HTML5 website for Kalypt.

## Run

```sh
export SUPABASE_URL="https://your-project-ref.supabase.co"
export SUPABASE_SECRET_KEY="your-server-side-secret-key"
export SUPABASE_INQUIRIES_TABLE="inquiries"
export INQUIRY_EMAIL_TO="estebanmarinramirez@icloud.com"
export SMTP_HOST="smtp.example.com"
export SMTP_PORT="587"
export SMTP_USERNAME="your-smtp-username"
export SMTP_PASSWORD="your-smtp-password"
export SMTP_FROM="notifications@your-domain.com"
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
SUPABASE_SECRET_KEY=your-server-side-secret-key
SUPABASE_INQUIRIES_TABLE=inquiries
INQUIRY_EMAIL_TO=estebanmarinramirez@icloud.com
SMTP_HOST=smtp.example.com
SMTP_PORT=587
SMTP_USERNAME=your-smtp-username
SMTP_PASSWORD=your-smtp-password
SMTP_FROM=notifications@your-domain.com
```

## Supabase

Run `supabase/schema.sql` in the Supabase SQL editor before enabling the contact form.

`SUPABASE_SECRET_KEY` must stay server-side. Do not expose it in browser JavaScript.

## Email Notifications

The contact endpoint saves each inquiry to Supabase, then sends an SMTP notification to `INQUIRY_EMAIL_TO`.

Use a transactional SMTP provider or a mailbox that allows app passwords. Keep `SMTP_PASSWORD` server-side in Render.
