# Review: feat/issue-22-collector-config-hash (#22)

**Date:** 2026-09-27 · **Scope:** `git diff main...HEAD` (8dfc806..2af0c05) · **Verdict:** APPROVE (P0/P1 нет)

## Проверено

- Python stdlib-only: новые импорты не добавлены (`hashlib`, `pathlib` уже были). `exit(0)` сохранён, резолв и хеш
  не бросают исключений (`OSError` перехватывается, для чтения frontmatter стоит `errors="replace"`).
- Запись state идёт под `AgentlogLock` (`fcntl.flock`), `State.save` делает `fsync` и атомарный rename. Pointer'ы
  изменяются только внутри блокировки.
- Резолв `agent_type` сделан по ADR D1: `rglob("*.md")`, индекс по frontmatter `name:`, fallback на stem,
  симлинки за пределы `.claude/agents/` и `.bak-` отбрасываются, при ambiguous возвращается `(None, N)` и пишется
  `ext.config_hash_ambiguous`. Снимок хеша делается на SubagentStop. Pointer'ы потребляются только после
  completion-гейта, поэтому незавершённый агент pointer не теряет. `_normalize_dispatch` переносит и `config_hash`, и `ext`.
- Go: `deployCollector` получил узкую сигнатуру, `DeployTelemetry` оборачивает ошибки через `%w`, `Apply`
  переиспользует его, поведение не меняется. Взаимоисключение `--telemetry-only` с аргументами overlay сделано в `Args`.
  API за пределы `internal/` не утекает. Новых зависимостей нет. Тесты table-driven, используют testify/require.
- Прогон: `go vet` + `go test` для apply/cmd/stats проходят, `test_config_hash.py` проходит (35 passed),
  `profiles/base/zprof-collect.py` совпадает с `.claude/zprof-collect.py`.
- Отклонение от AC4 (нет `.zprof.yaml`, вместо него `--telemetry-only`) обосновано фактами 5 и 6 в ADR. Согласен.
- `todo.md`: чекбокс #22 отмечен `[x]`. `followup.md` и untracked docs в diff не попали. Секретов нет.
  `.gitignore` покрывает `.agentlog/` и `.zprof/runs/`.
- Коммиты: `docs:`, `feat(base):`, `feat(cli):`, `test(cli):`, `docs(wiki):`. Конвенция соблюдена.

## Findings

### P2
1. **AC5 можно закрыть до завершения раннера.** `zprof score --latest` без `--no-collect` сам выполняет синтетический Stop
   (см. docstring `_collect_subagent_transcripts`). Ручной запуск из main-сессии после завершения любого вложенного
   диспатча соберёт pointer'ы и покажет карточку. Объяснение «курица и яйцо» верно только для
   Stop-хука, а не для AC5 в целом. Кодового гэпа нет, но live-проверку стоит сделать до мержа или в PR
   (после `make install`, чтобы бинарь был свежим).
2. **Индекс строится заново на каждого агента в collect-fallback.** Вызов `_agent_config_hash(cwd, agent_type)` внутри
   pass 2 (`zprof-collect.py:1184`) каждый раз заново делает rglob и читает все frontmatter. ADR D1 говорит «один раз на
   вызов коллектора». На SessionStart-recovery с десятками агентов это N×25 чтений. Корректность не страдает.

### P3
3. `_agent_frontmatter_name` принимает и вложенный `  name:` (делает strip перед `startswith`). Если во frontmatter будет
   вложенный блок с `name:` раньше верхнеуровневого ключа, резолв пойдёт неверно. Стоит проверять только строки без отступа.
4. `--telemetry-only --dry-run` всегда печатает 3 цели, а при base без телеметрии реально пишется 1 файл.
5. Ключи `ext.config_hash_source` / `ext.config_hash_ambiguous` не описаны в `telemetry.yaml`. ADR это допускает.
   Для discoverability их стоит упомянуть в комментарии раздела ext.
6. У ADR 0001 статус `proposed`. При мерже его нужно перевести в `accepted`.
7. Follow-up'ы из ADR Consequences (NamespaceAgent переписывает `name:`, `--telemetry-only` в `make install`)
   должны стать issues. Проверь, что они заведены.
