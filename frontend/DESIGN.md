# Kiro-Go console — UI conventions

Stack: TanStack Start (SPA mode) + TanStack Router (file routes) + TanStack Query,
shadcn/ui (new-york, Tailwind v4, Radix), lucide icons, sonner toasts.
The build is static and served by the Go binary under `/admin` (`npm run build`
publishes to `../web`). API calls go to `/admin/api/*` with the admin password
header — always use `src/lib/api.ts` (`get`/`post`/`put`/`del`), never `fetch` directly.

## Look and feel
- Calm neutral surfaces, one violet accent (`primary`) for primary actions and
  live signals. Semantic colours: `success`, `warning`, `destructive`, `info` —
  always paired with an icon or label, never colour alone.
- Hairline borders (`border`), rounded-xl cards, subtle `shadow-xs` at most.
- Typography: Inter; numbers use `tabular` class; IDs, keys and code use `font-mono`.
- Density: tables `text-sm`, rows ~44px; forms grouped in cards with a short
  description under each title.

## "Alive" without noise
- Page sections enter with `stagger` (children rise in sequence) or `animate-rise`.
- Live numbers use `useAnimatedNumber` / `StatCard`.
- Live state uses a pulsing dot (`animate-pulse-ring` + text colour).
- Loading uses `Skeleton` shaped like the content, never spinners alone.
- Every mutation gives feedback: optimistic update where safe, `toast.success`
  on completion, `toast.error(err.message)` on failure; buttons show a
  `Loader2` spinner and disable while pending.
- Destructive actions confirm via `useConfirm()` (`components/app/confirm.tsx`).
- Empty states use `EmptyState` with an icon, one sentence and a next action.
- Respect `prefers-reduced-motion` (the shared utilities already do).

## Structure
- Routes: `src/routes/_app/<page>.tsx` (already created). Keep route files thin;
  put page sections in `src/components/<area>/`.
- Data: React Query. Area hooks live in `src/lib/queries.<area>.ts`; shared keys
  in `qk` (`src/lib/queries.ts`). Invalidate the relevant keys after mutations.
- Types: `src/lib/types.ts` mirrors the backend wire format exactly.
- Text: every user-visible string goes through `useI18n().t(key)`. English
  strings for an area live in `src/locales/en.<area>.ts` with keys prefixed by
  the area (`accounts.…`). Chinese and Vietnamese fall back to English.
- Shared building blocks: `PageHeader`, `StatCard`, `EmptyState`, `CopyButton`,
  `CodeBlock`/`InlineCode`, `useConfirm`, formatting helpers in `lib/format.ts`.

## Accessibility
- Every icon-only button has `aria-label` (and usually a `Tooltip`).
- Form controls have `<Label htmlFor>`; dialogs have titles.
- Keyboard: everything reachable; ⌘K opens the command palette.
