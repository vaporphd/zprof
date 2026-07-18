# Kotlin overlay gap audit — zprof vs alcherk/claude-code-agents

**Date:** 2026-07-18
**Sets compared:**
- **A (zprof):** `profiles/overlays/kotlin-android/agents/` — 13 agents, 4,283 lines total
- **B (reference):** `github.com/alcherk/claude-code-agents/agents/coding/kotlin/` — 11 agents, 4,044 lines total (the master `kotlin-multiplatform-developer.md` alone is 1,532 lines)

## TL;DR

- **zprof is Android-only by design.** Its architect explicitly refuses KMP requests and routes them to another overlay (§12 must-not: "Do NOT reference iOS, Swift, KMP, UIKit, or SwiftUI. Wrong overlay."). The reference set is Android + KMP + Spring Boot in one folder.
- **For pure Android + Jetpack Compose + Hilt, zprof implementer covers MORE ground than the reference's `builder-compose.md`.** No topic in `builder-compose.md` is missing from zprof; zprof adds a full self-validation checklist, forbidden-imports denylist, per-file file-size caps, and much tighter Compose stability rules.
- **The big gaps are at OVERLAY scope, not agent scope:** zprof has zero coverage of Kotlin Multiplatform (expect/actual, source sets, Decompose, Ktor client, SQLDelight, Koin/Kodein) and zero coverage of Spring Boot server-side Kotlin. Both are legitimate topics but do NOT belong in `kotlin-android/` — they would need separate overlays (e.g. `kotlin-multiplatform/`, `kotlin-spring/`).
- **Four small implementer-level rules from `builder-compose.md` are worth porting into zprof implementer regardless of scope**: single JSON instance via DI, common-component threshold (5+ callsites → hoist to `ui/common/`), obtainEvent naming as an accepted synonym for onEvent, explicit `Result<Flow<T>>` mention.
- **Return-format contract diverges.** Reference agents have no schema-first `return_format:` block; they end each contract with a §OUTPUT FORMAT prose section. zprof's schema is stricter and more parseable.

---

## Set inventory — mapping

| zprof file (Set A) | Reference equivalent (Set B) | Coverage verdict |
|---|---|---|
| `implementer.md` (511 L) | `builder-compose.md` (237 L) + `builder-spring.md` (208 L) + `kotlin-multiplatform-developer.md` (1,532 L) | **A is superset for Android/Compose scope**; A has zero KMP/Spring |
| `architect.md` (433 L) | folded into `kotlin-multiplatform-developer.md` §1–§4 | A has an explicit architect role; B collapses it into the developer |
| `reviewer.md` (415 L) | folded into `builder-*` agents + `security-kotlin.md` (218 L) | A has a dedicated categorized reviewer; B distributes review responsibility |
| `tester.md` (359 L) | `test-spring.md` (189 L) — Spring only | **A covers Android tests; A does NOT cover Spring testing** |
| `bug-hunter.md` (418 L) | `diagnostics-kotlin.md` (282 L) | A is deeper on runtime debugging; B focuses on diagnostic tooling |
| `refactor-agent.md` (415 L) | `refactor-mobile.md` (59 L) + `refactor-spring.md` (310 L) | A is Android-refactor complete; A has NO Spring refactor |
| `explorer.md` (305 L) | *no equivalent* | A adds an inventory/read-only role that B lacks |
| `init-android.md` (607 L) | `init-kotlin.md` (569 L) | Comparable size; A is Android-scoped, B is KMP-scoped |
| `gradle-runner.md` (155 L) | *distributed across builder-\* agents* | A externalizes the runner as a tool-agent (better decomposition) |
| `adb-driver.md` (186 L) | *no equivalent* | A adds device-side automation B lacks |
| `emulator-driver.md` (178 L) | *no equivalent* | A adds emulator automation B lacks |
| `detekt-checker.md` (154 L) | *no equivalent* | A externalizes; B implies inline |
| `ktlint-checker.md` (147 L) | *no equivalent* | A externalizes; B implies inline |
| *no equivalent* | `devops-orchestrator.md` (291 L) | B has a CI/CD orchestrator; A relies on base-overlay orchestrator |
| *no equivalent* | `security-kotlin.md` (218 L) | A folds security into reviewer §security; B has a dedicated agent |
| *no equivalent* | `system-analytics.md` (149 L) | A has no analytics coverage at all — see §Gap 3 below |
| *no equivalent* | `kotlin-multiplatform-developer.md` (1,532 L) | A has zero KMP coverage — see §Gap 1 |
| *no equivalent* | `builder-spring.md` + `refactor-spring.md` + `test-spring.md` | A has zero Spring coverage — see §Gap 2 |

## Implementer gap analysis — the user's core question

The user's IMPLEMENTER concern maps against three reference agents that together carry implementer duties: `builder-compose.md`, `builder-spring.md`, `kotlin-multiplatform-developer.md`.

### Topic coverage matrix

| Topic | zprof implementer.md | Reference | Verdict |
|---|---|---|---|
| Coroutines / Dispatchers injection | §5.1 exhaustive (IoDispatcher/DefaultDispatcher/MainDispatcher qualifiers, DI, `withContext` in Repository only) | `kotlin-multiplatform-developer.md` §7.2 (only Dispatchers.Main), `builder-compose.md` none | **A stronger** |
| Flow / StateFlow / SharedFlow / Channel | §5.2 explicit (cold flow up-tree, `.stateIn(vm, WhileSubscribed(5s), initial)`, Channel for effects) | `kotlin-multiplatform-developer.md` §7 (Channel for SideEffect only) | **A stronger** |
| ViewModel structure | §3.3 — HiltViewModel, `_state`/`state`, `_effect`/`effect`, `onEvent(Event)` | `kotlin-multiplatform-developer.md` §7 — Decompose Component (not ViewModel), `obtainEvent(Event)` | **Different pattern** — see §Note below |
| UseCase contract | §3.4 — `suspend fun execute(params): Result<T>`, no `operator fun invoke`, catch-and-map in execute | `kotlin-multiplatform-developer.md` §6 + `builder-compose.md` §4 — identical rule set | **Match** |
| Repository — concrete class, no interface | §3.5 default is concrete class, interface only when ADR calls for it | `kotlin-multiplatform-developer.md` §4 — MANDATORY concrete class (no ADR carve-out) | **B slightly stricter** — zprof allows an interface if ADR justifies |
| Error handling / sealed error hierarchy | §3.4 example uses per-feature `<Name>Error`, `try/catch` inside `execute` | `kotlin-multiplatform-developer.md` §5 — dedicated section, sealed class with data objects | **Match on rule, B has richer example** |
| Compose Screen / Content / stateless UI | §3.1 + §6 — Screen thin adapter, Content stateless, `collectAsStateWithLifecycle`, `LaunchedEffect` for effects | `builder-compose.md` §1–§2 — Screen thin, View pure (no `remember` at all) | **A more nuanced** — A permits local UI-only remember, B forbids all |
| DTO ↔ Domain mapping | §3.6 — dedicated Mapper class in `data/mapper/`, DTO snake_case via `@SerialName` | `kotlin-multiplatform-developer.md` §9 — Mapper object with extension functions, `Clock.System.now()` | **Match on principle; B example uses kotlinx-datetime, A example doesn't** |
| DI (Hilt, per-feature module) | §5.3 — `@Module @InstallIn(SingletonComponent::class) object <Feature>Module`, `@Provides` for interfaces, `@Inject constructor` for concrete | `kotlin-multiplatform-developer.md` (Kodein/Koin — not Hilt); `builder-compose.md` §DI (mention only) | **A stronger for Android; B stronger for KMP** |
| File-size caps | §7 — 1000 red / 600 yellow / method 100 lines / `@Composable` 100 lines | `builder-compose.md` §1 — 1000 red / 600 yellow only | **A stronger** (method + composable caps) |
| Layer denylist / forbidden imports | §3.7 — full per-layer denylist table + detekt.yml enforcement | none in reference | **A stronger** — a real gap on the reference side |
| Self-validation checklist | §11 — 32-item checklist scoped per section | none in reference | **A stronger** — reference has prose validation only |
| Return-format schema | frontmatter `return_format:` YAML block + §OUTPUT FORMAT §10 | §OUTPUT FORMAT prose only, no schema | **A stronger** |
| Version-pin discipline | implementer §0.3 defers new libs to architect; architect pins `libs.versions.toml` | not enforced anywhere in reference | **A stronger** |
| Commit contract | §9.10 — Conventional Commits `feat(<module>):`, `git add feature/<name>/`, no `-A` | none in reference | **A stronger** |

### Note on ViewModel vs Component

zprof uses **Jetpack ViewModel + Hilt** (Android-native). Reference `builder-compose.md` + `kotlin-multiplatform-developer.md` use **Decompose Component** because they target Compose *Multiplatform*, where the Android-specific `ViewModel` is not available in commonMain.

For the Android-only scope zprof commits to, ViewModel is the correct choice. If Alex ever wants KMP coverage, a new overlay (`kotlin-multiplatform/`) with a Component-based agent set is the right shape — not shoehorning Decompose into `kotlin-android/`.

### Concrete implementer rules worth porting into zprof

Four rules from `builder-compose.md` / `kotlin-multiplatform-developer.md` are not KMP-specific and would tighten zprof implementer:

1. **Single JSON instance via DI** (`builder-compose.md` §10, `kotlin-multiplatform-developer.md` §10.1) — every `Json { … }` block in the codebase is a review-blocker; the instance lives once in `core/network/di`. zprof mentions kotlinx.serialization but doesn't enforce single-instance.
2. **Common-component threshold** (`builder-compose.md` §9) — a Composable used in 5+ places must be moved to `core/ui/common/<Name>.kt`. zprof has file/method size caps but no "duplication → hoist" rule.
3. **`obtainEvent(Event)` accepted synonym for `onEvent`** (`kotlin-multiplatform-developer.md` §7.1) — either name is fine, both are greppable single entry points. Not strictly a rule to add, but should be recognised so zprof doesn't reject reference-style code as violating §3.3 "one public event entry point named onEvent".
4. **Explicit `Result<Flow<T>>` variant in UseCase** — zprof §3.4 mentions it in one line ("or `Result<Flow<T>>` when the action naturally streams") but does not show an example. Reference §6 gives one. Add a brief example so the implementer doesn't reach for a naked `Flow<T>` return.

## File-level gaps

| Question | Answer |
|---|---|
| Does A have an equivalent to `init-kotlin.md`? | Yes — `init-android.md` (607L vs 569L). A is Android-scoped, B is KMP-scoped. Different but overlapping. **No gap for A's scope.** |
| Does A have an equivalent to `security-kotlin.md`? | Distributed — reviewer §security covers WKWebView-equivalent (WebView), Keychain-equivalent (EncryptedSharedPreferences), Keystore, deep-link injection, HTTP security. **Adequate but not a dedicated agent.** Consider consolidating into a `security-checker.md` tool-agent for parity with detekt-checker/ktlint-checker. |
| Does A have an equivalent to `diagnostics-kotlin.md`? | Yes — `bug-hunter.md` (418L vs 282L). A is deeper. **No gap.** |
| Does A have an equivalent to `system-analytics.md`? | **No.** See §Gap 3 below. |
| Does A have an equivalent to `devops-orchestrator.md`? | Base overlay carries `dev-orchestrator.md` + `exploratory-orchestrator.md`. Overlaps but not identical — reference version is Kotlin-CI-specific (Gradle build cache, Github Actions, Play publishing). Consider adding a `ci-driver.md` if Play Store shipping becomes in-scope. |
| Does A's `refactor-agent.md` cover both mobile AND server-side? | Android/Kotlin mobile only. **Gap for Spring-side refactor** — but this is out of scope for `kotlin-android/`. |
| Does A's `tester.md` cover Spring/WebFlux testing? | No — Android-only (JUnit4, MockK, Turbine, Compose UI test, Robolectric, Espresso). **Gap only if server-side Kotlin becomes in-scope.** |

## Gap 1 — Kotlin Multiplatform (major)

The reference has a 1,532-line `kotlin-multiplatform-developer.md` that covers:
- Source-set structure (commonMain / androidMain / iosMain / desktopMain / webMain)
- `expect`/`actual` for platform primitives — MUST live in `core/`, NEVER in `feature/`
- Decompose Component navigation (StackNavigation, SlotNavigation, childStack, childSlot, Config sealed classes, RootComponent)
- Ktor HttpClient (per-platform engines: OkHttp on Android, Darwin on iOS, CIO on desktop, Js on web)
- SQLDelight drivers (per-platform via expect/actual DatabaseDriverFactory)
- Kodein / Koin DI (not Hilt — because Hilt is JVM-only)
- Compose Multiplatform + optional SwiftUI/UIKit/Vue/React/Angular UI

**zprof has ZERO of this.** The architect explicitly bounces KMP requests to another overlay — but that overlay does not exist yet.

**Recommendation:** if KMP is worth supporting, create a NEW overlay `profiles/overlays/kotlin-multiplatform/` with a Decompose-based Component agent, expect/actual rules, and Ktor+SQLDelight networking/persistence. Do NOT merge into `kotlin-android/`. The two toolchains are genuinely different.

## Gap 2 — Spring Boot server-side (major)

The reference has `builder-spring.md` + `refactor-spring.md` + `test-spring.md` covering:
- Spring Boot feature-slice structure (`api/endpoint/`, `api/schema/`, `service/`, `persistence/`, `domain/`)
- Controller/Service/Repository/Domain layer rules with cyclic-dependency ban
- Spring Data JPA, `@RestController`, `@Service`, `@Repository`, `@Entity`
- Request/Response DTO naming (`<Feature><Action>Request.kt`, `<Feature><Action>Response.kt`)

**zprof has ZERO of this.** Spring is server-side Kotlin — genuinely a different world from Android Kotlin.

**Recommendation:** if Spring is worth supporting, create a NEW overlay `profiles/overlays/kotlin-spring/`. Do NOT merge into `kotlin-android/`. Coroutine models diverge (WebFlux vs `viewModelScope`), DI diverges (Spring vs Hilt), persistence diverges (JPA vs Room), testing diverges.

## Gap 3 — Analytics coverage (minor)

The reference `system-analytics.md` covers:
- Firebase / Amplitude / Mixpanel event schema
- Event naming conventions (snake_case, verb_object)
- Screen tracking
- User properties vs event properties
- GDPR/PII redaction rules for analytics

**zprof has zero.** Analytics is a common ask across mobile projects. Consider adding an `analytics-guide.md` reference document under docs, or a lightweight `analytics-planner.md` agent.

## KMP coverage (§3)

**Zero.** zprof `kotlin-android/architect.md` §12 explicitly bans KMP topics: "Do NOT reference iOS, Swift, KMP, UIKit, or SwiftUI. Wrong overlay." — but there is no "right overlay" yet.

## Spring / server-side coverage (§4)

**Zero.** No agent in `kotlin-android/` mentions Spring, WebFlux, R2DBC, `@RestController`, or Spring Data. Coverage would require a new overlay.

## Return-format shape comparison (§5)

- **zprof**: every agent has a `return_format:` YAML block-literal in frontmatter (`verdict: done|blocked|failed` / `artifact:` / `next:` / `one_line:` / `confidence:` / `self_check:` / `notes:`). Enforced by a `# CRITICAL:` scaffold comment inside the block. The eval binary parses this deterministically.
- **reference**: no frontmatter `return_format:` field. Each agent ends with a §OUTPUT FORMAT prose section describing markdown headings (Summary, Folder tree, File list, Full code, Architecture validation). Not schema-first. Not machine-parseable without an LLM pass.

**zprof is significantly stronger here.** No port needed; reference is behind.

## Model tier comparison (§6)

- **zprof**: `model: sonnet` on implementer/tester/refactor-agent/etc; `model: opus` on architect/reviewer/planner (schema-forward roles).
- **reference**: `model: sonnet` on every agent surveyed. No opus escalation for reasoning-heavy roles.

**zprof is more nuanced.** Alex's own `sonnet5_narrate_first_prior` memory documents the empirical rationale for the tier split.

---

## Recommended action items

Priority-ordered — most impactful first.

### High priority

1. **Decide overlay scope.** If Alex intends to support KMP or Spring Kotlin, create SEPARATE overlays (`kotlin-multiplatform/`, `kotlin-spring/`) — don't collapse them into `kotlin-android/`. Current architect §12 ban is correct; it just needs actual overlay targets to point at.
2. **Port the four small implementer rules** identified in §Concrete implementer rules worth porting (single JSON instance, common-component threshold, obtainEvent synonym, `Result<Flow<T>>` example). Small tightening, no scope shift.

### Medium priority

3. **Add an `analytics-planner.md` agent** or a `docs/analytics-conventions.md` reference. Firebase/Amplitude event schema is a common gap in Android projects.
4. **Consider consolidating security into a `security-checker.md` tool-agent** (parallel to `detekt-checker.md` + `ktlint-checker.md`). Currently reviewer §security is the sole path; a dedicated tool-agent would enable ad-hoc security passes without a full reviewer run.

### Low priority

5. **Cross-reference `kotlin-android/`'s architect §12 "wrong overlay" language with an actual pointer** — when the new KMP overlay lands, update the architect to name it: "For KMP shared code, use `kotlin-multiplatform` overlay."
6. **Add a CI-driver tool-agent** (`ci-driver.md` for Github Actions / Gradle build cache / Play publishing) if Play Store shipping is in-scope. Similar to base overlay's `dev-orchestrator.md` but Kotlin-specific.

### Non-actions (documented rejections)

- **Do NOT rewrite zprof implementer around Decompose.** ViewModel is correct for Android-only scope; Decompose is correct for KMP scope. They serve different worlds.
- **Do NOT adopt reference's non-schema return format.** zprof's frontmatter `return_format:` block is measurably stronger — it feeds `zprof eval` deterministically, which the reference cannot.
- **Do NOT split `refactor-agent.md` into `refactor-mobile.md` + `refactor-spring.md`** as reference does. Only makes sense if Spring becomes in-scope.

---

## Nothing important is missing from IMPLEMENTER for Android scope

Direct answer to the user's question. Within its declared Android scope, zprof implementer covers every topic `builder-compose.md` teaches and adds:
- Full forbidden-imports denylist per layer (reference: none)
- 32-item self-validation checklist (reference: prose validation)
- Dispatchers-via-DI discipline with 3-qualifier pattern (reference: mentions Main only)
- Compose stability rules — `@Immutable` UiState, `ImmutableList`, Modifier ordering, explicit `LaunchedEffect` keys (reference: partial)
- File-size and method-size caps with automated flagging (reference: file-size only)
- Kotlinx.serialization enforcement (reference: mentions only)
- Version-pin discipline via `libs.versions.toml` handoff to architect (reference: none)
- Commit-message contract (reference: none)

The four minor rules noted in §"Concrete implementer rules worth porting" are the only concrete additions worth making, and even those don't change any behaviour — they just tighten style consistency.
