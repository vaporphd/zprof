stack:
  frontend-web:
    lang: typescript
    node_version: "22.x LTS"            # 20+ minimum; 22 preferred
    ts_version: "5.6+"
    package_manager: "pnpm"             # or npm / yarn / bun — detect from lockfile
    framework: "vue3"                   # or nextjs / nuxt3 / react-vite / astro / svelte-kit
    vue_version: "3.5+"                 # if vue
    next_version: "15.x"                # if next; App Router mandatory for new
    react_version: "19.x"               # if next 15 or react-vite
    ui_kit: "tailwindcss + shadcn/ui"   # or vuetify / element-plus / mui / chakra
    build_tool: "vite-6"                # or webpack (legacy Next) / turbopack (Next 15)
    dev_server_cmd: "pnpm dev"
    build_cmd: "pnpm build"
    test_unit_cmd: "pnpm test"          # vitest
    test_e2e_cmd: "pnpm test:e2e"       # playwright
    lint_cmd: "pnpm lint && pnpm typecheck"
    format_cmd: "pnpm format"           # prettier
    install_cmd: "pnpm install --frozen-lockfile"
