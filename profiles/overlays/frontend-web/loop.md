## Frontend / Web (Vue3 + Next.js) loop extension

Расширение dev-pipeline для веб-фронта.

### Trigger phrases
- EN: `next frontend task`, `add page`, `add component`, `run dev`, `run e2e`, `build prod`
- RU: `следующая задача`, `добавь страницу`, `добавь компонент`, `запусти dev`, `прогони e2e`, `собери prod`

### Pipeline
Стандартный dev-pipeline. architect/implementer/tester/refactor-agent/bug-hunter/reviewer/explorer знают:
- Vue 3.5+ Composition API (`<script setup lang="ts">`, `defineProps`/`defineEmits`, `ref`/`reactive`/`computed`, `watch`/`watchEffect`)
- Next.js 15 App Router (Server Components by default, `use client` для интерактивности, Server Actions, `page.tsx`/`layout.tsx`/`loading.tsx`/`error.tsx`)
- React 19 (auto-memoization via compiler, `use()` для промисов, Suspense boundaries)
- TypeScript 5.6+ strict (`noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`)
- Vite 6 / Turbopack (в Next 15)
- Pinia (Vue state) / Zustand (React) / TanStack Query (server state) / RTK Query
- Vue Router 4 / Next.js App Router
- TailwindCSS 4 + shadcn/ui или equivalent design system
- Vitest 2 (unit) + Playwright 1.48+ (e2e)
- ESLint 9 flat config + Prettier 3

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Установка / обновление пакетов | `pnpm-manager` |
| Dev-сервер / prod-сборка | `vite-runner` |
| Unit tests + coverage | `vitest-runner` |
| E2E tests (Playwright) | `playwright-runner` |
| Линт (ESLint + Prettier check) | `eslint-checker` |
| TypeScript check (`tsc --noEmit`) | `tsc-checker` |

### Изоляция — специфичные правила
- **`pnpm-lock.yaml` может быть 10k+ строк** — использовать `pnpm ls <pkg>` или `pnpm why <pkg>` вместо чтения целиком.
- **`node_modules/` и `dist/` НИКОГДА не читаем** — только исходники + config-файлы.
- **`.next/` и `.nuxt/` — тоже не читаем** — генерированный кэш.
- **Vite HMR output может флудить** — dev-сервер запускать с `--logLevel error`, читать только ошибки.
- **Playwright traces** — при failure может быть 100+ MB; parser отдаёт только скриншот last failed + trace path.
- **Next.js build output** может быть 5k+ строк — `vite-runner` возвращает только errors + bundle size summary.
- **Не запускать `pnpm install` с `--force` без ask** — уничтожает лочку.
- **Не запускать `pnpm dlx create-next-app` / `create-vite` в непустой папке** — конфликт.
- **Никогда не коммитить `.env.local`, `.env.production`** — только `.env.example`.
- **Server Components / Client Components boundary** — не открывать полный call graph в контекст; использовать `implementer` + `architect` для решения `"use client"` границы.
