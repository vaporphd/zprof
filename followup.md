# Followup

## Status
- 2026-09-27/28: Plan 1 закрыт кроме #17 (Alex) + #53/#60/#62. Plan 2 guard: #23–#30 + #52 + #56 done (PR #48–#63, #30 в PR, ждёт merge); осталось #31; #47, #59. Решения Alex: auto-merge везде; --delete-branch остаётся; required CI; guard fail-open, deny везде (кроме branch_pr_merged).
- Score 80/100 · Solid · done · 2026-09-28-issue-29-doctor-guard-checks · confidence full
  106.3M tok · 13 dispatch · 592 tool calls · 45 min · sonnet×12 opus×2 · −10 P7 32 (guard: 11 deny/block) · −3 P6 7% (fork) · −3 P3 fork 14/84 Read
- #30 review: 0 P0/P1, follow-up backlog 1×P2 (pip_install захардкожен в test_guard.py, не читается из overlay guard.yaml) + 3×P3 (pr-shepherd.md длина, regex матчит uv pip install, exempt_roles.publish без e2e).

## Next
- pr-shepherd мержит PR #30. Затем #31 shakedown (ноль ложных deny guard, depends: #24–#30, все закрыты); #47, #53, #59, #60, #62.
- Вопросы Alex: деплой-копии (#59) — трекать или `git rm --cached`; NORTH_STAR.md для zprof (#60); #17; старые remote-ветки PR #3–#9.
- Local main = origin/main (2df84ae). Untracked docs/*, thoughts/, tasks/plan-issue-*.md, .claude/zprof-guard.py, guard.json не трогать.
