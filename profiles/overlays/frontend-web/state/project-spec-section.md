## Frontend / Web section (для PROJECT_SPEC.md)

### Toolchain
- Node.js: <e.g. 22.11.0 LTS> — from `.nvmrc` or `package.json` engines
- Package manager: pnpm <version> (recommended) / npm / yarn / bun
- TypeScript: <e.g. 5.6.3>
- Build tool: Vite <version> / Next.js Turbopack / Webpack (legacy)

### Framework
- Primary: Vue 3.5+ / Next.js 15 App Router / Nuxt 3 / React + Vite / Astro / SvelteKit
- Router: Vue Router 4 / Next App Router / Nuxt file-based / TanStack Router
- Rendering: SSR / SSG / SPA / ISR — chose ONE per route or per app

### State management
- Local: `ref`/`reactive` (Vue) / `useState`/`useReducer` (React)
- Global: Pinia (Vue) / Zustand / Redux Toolkit (React)
- Server state: TanStack Query / SWR / RTK Query
- Forms: VeeValidate + Zod (Vue) / react-hook-form + Zod (React)

### Styling
- Utility-first: Tailwind CSS 4 (recommended)
- Component library: shadcn/ui (React) / shadcn-vue / Radix UI / Vuetify / Element Plus / MUI / Chakra
- CSS-in-JS: avoided (perf); use CSS Modules or Tailwind
- Icons: lucide-react / lucide-vue-next

### API integration
- HTTP: `fetch` (native) with typed wrapper / `ofetch` (Nuxt/generic) / axios (legacy)
- GraphQL: `@apollo/client` / Villus (Vue) / URQL
- Codegen: openapi-typescript for REST types; graphql-codegen for GraphQL

### Testing
- Unit: Vitest 2 + @testing-library/vue or @testing-library/react
- E2E: Playwright 1.48+ (recommended) or Cypress
- Component: Storybook 8 (optional)
- Coverage: Vitest built-in (V8) — target `>= 70%` for utils, `>= 50%` for components

### Quality
- Linter: ESLint 9 flat config with `@typescript-eslint`, `vue/`, `next/`, `react-hooks/`, `jsx-a11y/`
- Formatter: Prettier 3 with `--config` shared with monorepo if applicable
- Type checker: `tsc --noEmit` (mandatory in CI)
- Bundle analyzer: `rollup-plugin-visualizer` (Vite) / `@next/bundle-analyzer`
- Perf: Lighthouse CI budget, Web Vitals monitoring

### Deployment
- Vercel / Netlify / Cloudflare Pages / static bucket + CDN / Docker+nginx
- Container: multi-stage Node → distroless (if SSR) OR pure static
- Health: `/health` route (SSR) or synthetic (SSG)

### CI
- GitHub Actions / Vercel deployment previews / GitLab CI
- Steps: pnpm install --frozen-lockfile → lint → typecheck → test → build → e2e (against preview URL)
- Cache: pnpm store, .next/cache, Playwright browsers

### Known caveats
- <e.g. Server Components can't use hooks; must be async fn>
- <e.g. Vue 3 reactivity loses type on destructure of `reactive`, use `toRefs`>
