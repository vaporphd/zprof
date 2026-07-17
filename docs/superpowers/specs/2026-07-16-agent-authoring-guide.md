# Agent Authoring Guide

Standard for full-fledged Claude Code subagents in zprof overlays. Based on a deep read of AlexGladkov/harnest and vaporphd/claude-code-agents.

## What "full-fledged" means

A full-fledged agent is a **self-contained instruction manual** for one narrow role in one specific stack. It carries:

- Enough domain knowledge that the LLM does not have to guess conventions, versions, or layer rules
- Explicit workflow steps in the order they must be executed
- Explicit output-format contract (the shape of the reply the agent must produce)
- Explicit negative rules (things never to do)
- Concrete file paths, command lines, code snippets, and version numbers — not abstractions

**Target length: 200-400 lines** for standard executor agents. **500-800 lines** for scaffolders, orchestrators, and multi-target agents (e.g. KMP-scale). Under 150 lines only for pure-checklist agents (`refactor-mobile` shape) or extremely narrow tool wrappers.

**A 2-3 line agent is not an agent** — it is a placeholder. Never ship one.

## Mandatory frontmatter

```yaml
---
name: <kebab-case-slug>
description: <one long sentence with role scope + trigger phrases (bilingual RU/EN aliases if user speaks RU)>
model: <opus | sonnet | haiku>
color: <blue | green | purple | red | orange | cyan>  # optional convention (see below)
return_format: |
  verdict: done|blocked|failed
  artifact: <path>
  next: <role> | null
  one_line: <≤120 chars>
---
```

**Color convention (steal from vaporphd):** `blue`=scaffolder/planner, `green`=backend generator/coordinator, `purple`=UI generator/refactor, `red`=diagnostics/YOLO, `orange`=security/devops, `cyan`=multiplatform/spec-writer.

**Model tier default:** `sonnet` for executors, `opus` for orchestrators and architect/reviewer/bug-hunter/refactor (roles requiring deep reasoning), `haiku` for mechanical tool wrappers and report writers.

**`tools:` line:** omit for maximum flexibility (all tools). Restrict only when the agent must be truly read-only (explorer, reviewer read-pass).

## Required sections (in order)

Every agent >150 lines must have these sections. Shorter agents can omit sections marked (opt).

### 1. Opening role paragraph
One paragraph: "You are a specialized <role> agent for the <stack> overlay. You <primary responsibility>. You do NOT <what belongs to sibling roles>." Establish scope, name sibling agents, name the artifacts you produce.

### 2. Global Behavior Rules (or "Section 0: HARD RULES")
5-10 bullet lines of non-negotiable posture. Examples:
- "Never modify production code. Tests verify current behavior, even if buggy." (test-spring)
- "Never propose fixes. Diagnose only, then ask for approval before writing." (bug-hunter)
- "Do not fetch versions from your own knowledge. Web-lookup latest and print Version Resolution Summary." (devops)

### 3. Mandatory Initial Dialogue (opt for context-heavy agents)
Numbered list of questions to ask before doing any work, in exact asking order. Each question gets a default that applies if the user replies "default"/"skip". Skip this section for agents that operate purely on artifacts (todo.md, plan-N.md, PROJECT_SPEC.md).

### 4. Domain / strict rules
The bulk. Subdivided per layer, per artifact class, or per phase. Concrete allow-lists AND deny-lists per class:
```
Controller — May depend on: <Feature>Service only.
             MUST NOT depend on: Repository, Entity, DTO of another feature, Service of another feature.
```

### 5. File-size / one-type-per-file constraints
Exact numbers: red zone 1000 lines, yellow zone 600, method cap 100. Split recipe per language.

### 6. Workflow
Numbered execution order. If phases exist, name them (Phase 1: static scan / Phase 2: auto-commands / Phase 3: instrumentation / Phase 4: runtime / Phase 5: bug localization). Include literal bash commands per phase where applicable.

### 7. Output Format
Numbered contract for the final reply. Example (builder-spring):
```
1. Summary — what feature was generated, which layer
2. Folder tree — files created (paths only)
3. File list — one line per file with layer classification
4. Full code — every file in a fenced block, no ellipsis
5. Validation checklist — self-report ✅/❌ against §21
```

### 8. Things You Must Not Do (Safety Rules)
Closing negative list. Catches failure modes the positive rules miss.

### 9. (opt) Multilingual approval-trigger bank
Only for agents that gate a destructive step behind explicit consent. Provide:
- English: "OK / Yes / Do it / Apply / Fix / Confirm"
- Russian: "OK / Да / Исправь / Применяй / Фикс / Го / Давай"
- Semantic examples: "yeah go ahead", "sure fix it", "давай сделай", "окей поехали"

### 10. (opt) Self-validation checklist
Final workstream step. 20-40 boolean items the agent must self-report against with ✅/❌ before returning verdict. Highest-value pattern from KMP §21.

## The 10 depth cues — checklist for reviewing agent quality

Before shipping an agent, verify it hits at least 7 of these:

- [ ] Pins exact versions (e.g. "Kotlin 2.0.20", "Compose 1.7.x", not "modern Kotlin")
- [ ] Enumerates forbidden imports/APIs (blacklists, not principles)
- [ ] Has explicit Output Format section
- [ ] Has closing Safety Rules / Must Not Do list
- [ ] Has self-validation checklist at end
- [ ] Uses literal runnable commands, not descriptions
- [ ] Has Mandatory Initial Dialogue (for context-heavy agents)
- [ ] Has bilingual approval trigger bank (for gated destructive agents)
- [ ] Uses concrete file-size thresholds (red/yellow zones, method cap)
- [ ] Has allow-list AND deny-list per artifact class

## What NOT to do

- **Do not write "follow best practices"** — enumerate the practices with concrete rules.
- **Do not write "handle edge cases"** — name the edge cases you require handling for.
- **Do not write "write clean code"** — spell out what clean means for this stack (file size, layer rules, naming).
- **Do not stub sections with `TBD` or `see docs`** — write the actual content.
- **Do not reference external URLs as substitute for content** — the agent should be self-sufficient if the LLM has no web.
- **Do not repeat the entire base agent's rules** — reference `[[planner]]`, `[[reviewer]]` etc. by role name; the base pack ships them.
- **Do not use exact model IDs (claude-opus-4-8)** — always use tier aliases (opus/sonnet/haiku). The resolver maps them.

## Naming inside overlays

Overlay-role agents (architect, implementer, tester, bug-hunter, refactor-agent, explorer, reviewer) — same filename `architect.md`, `implementer.md`, etc.

Composition suffix when the same role coexists across overlays (e.g. iOS + Android in one project) — CLI appends namespace: `implementer-ios`, `implementer-android`. The suffix is added by the composer, not by the file name in the overlay.

Tool-agents — descriptive name matching what the tool does: `gradle-runner`, `xcode-runner`, `adb-driver`, `emulator-driver`, `ktlint-checker`, `swiftlint-checker`, etc.

## Reference material

Full reference survey at:
`/private/tmp/claude-501/-Volumes-mydata-z0mi-harness/52353252-b350-486d-9ad5-678f47053824/scratchpad/reference-survey.md`

Source repos (cloned locally, read-only):
- `/private/tmp/claude-501/-Volumes-mydata-z0mi-harness/52353252-b350-486d-9ad5-678f47053824/scratchpad/refs/vaporphd/`
- `/private/tmp/claude-501/-Volumes-mydata-z0mi-harness/52353252-b350-486d-9ad5-678f47053824/scratchpad/refs/harnest/`

When writing a new agent, first read the equivalent vaporphd agent for its stack (mapping in `reference-survey.md` Section "Role → vaporphd source mapping").
