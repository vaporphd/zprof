# Followup

## Status
- 2026-09-27/28: Plan 1 закрыт кроме #17 (Alex). Plan 2 guard: #23 PR #48/#49, #24 PR #50, #25 PR #51 (merge_preflight + pr_create_gate, ADR-0006, 425 pytest). Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- 2026-09-28: #26 guard — валидатор return_format на SubagentStop: режим subagent-stop в zprof-guard.py, ADR-0007, 76 новых тестов (коммиты e86d960/788e4c4/177539f/b82702d). PR TBD.
- Score 47/100 · Lucky · done · 2026-09-28-issue-25-guard-merge-pr-gate · confidence full
  59.9M tok · 14 dispatch · 346 tool calls · 67 min · sonnet×11 opus×4 · −15 P2 task-runner 15× `sleep 180` · −15 P6 58% без результата (9 ролей) · −10 P7 15 нарушений
- Два сигнала для разбора: (1) task-runner ждёт async-детей через `sleep 180` — 45 мин простоя за run, P2 считает это слепым повтором; (2) P6 50%+ в незаваленных run'ах #23/#25 — подозрение, что вложенные async-диспатчи в транскрипте раннера не сшиваются (#34 починил только main).

## Next
- В работе: #27 (guard-события в zprof score и zprof stats). Затем #28 → #29, #30 → #31; #47.
- Завести issue по (2) после проверки .agentlog; (1) — в разбор после Plan 2. #17 и старые remote-ветки — Alex.
- Local main = origin/main (03f3757). Untracked docs/*, thoughts/, tasks/plan-issue-*.md не трогать.
