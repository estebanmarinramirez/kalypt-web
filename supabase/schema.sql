create extension if not exists pgcrypto;

create table if not exists public.inquiries (
  id uuid primary key default gen_random_uuid(),
  created_at timestamptz not null default now(),
  name text not null,
  email text not null,
  focus text,
  message text not null,
  user_agent text,
  remote_addr text
);

alter table public.inquiries enable row level security;
