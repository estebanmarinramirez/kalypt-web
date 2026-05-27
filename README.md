# Kalypt

Minimal Go and HTML5 website for Kalypt.

## Run

```sh
export SUPABASE_URL="https://your-project-ref.supabase.co"
export SUPABASE_SECRET_KEY="your-server-side-secret-key"
export SUPABASE_INQUIRIES_TABLE="inquiries"
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
```

## Supabase

Run `supabase/schema.sql` in the Supabase SQL editor before enabling the contact form.

`SUPABASE_SECRET_KEY` must stay server-side. Do not expose it in browser JavaScript.
