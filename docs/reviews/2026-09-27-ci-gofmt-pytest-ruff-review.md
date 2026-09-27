# Review: 14-ci-gofmt-pytest-ruff (issue #14)

- Scope: `git diff main...14-ci-gofmt-pytest-ruff` — 35 файлов, +204/−148
- Commits: c69911b `style(cli): gofmt`, 7cc3fb5 `ci: gofmt/pytest/ruff gates in existing test job (#14)`
- Reviewer: reviewer (opus), run `.zprof/runs/2026-09-27-ci-pytest-gofmt-ruff.md`, audit_step 4
- Verdict: **clean** — P0: 0, P1: 0, P2: 3, P3: 4

## Gate (перепроверено локально @ 7cc3fb5)

- `cd cli && gofmt -l .` — пусто
- `go vet ./... && go test -race -count=1 ./... && go build ./...` — green
- `python3 -m pytest profiles/base/tests/ -q` — 180 passed (Python 3.14 локально)
- `uvx ruff check profiles/base/` (ruff 0.16.9) — All checks passed
- `profiles/base/telemetry_test.py` — 1 passed

## Чеклист

- Go: изменения в `cli/` — чистый gofmt (выравнивание полей/комментариев, одна пустая строка в shakedown.go). Семантика не тронута. Новых зависимостей нет, `go.sum` не менялся.
- Python stdlib-only: новых import'ов нет; в `zprof-collect.py` только разбиение `import a, b, c` на строки, сортировка `pathlib` после `datetime`. `exit(0)`-контракт, `fcntl.flock`, `os.fsync()` не затронуты (`writelines` остаётся перед `flush()`+`fsync()`).
- ruff-фиксы семантически эквивалентны: `startswith((a, b))`, `check=False` в `subprocess.run` (поведение по умолчанию то же), `writelines(genexpr)`, неиспользуемые `log_text` → `_`. Рефактор `score_defaults` в `telemetry_test.py` в walrus-`elif` проверен вручную: после ветки нет других `elif`, fall-through эквивалентен исходному.
- ruff — CI-only (`pip install` в ci.yml), не runtime-зависимость коллектора. per-file-ignores BLE001/S110 ограничены `zprof-collect.py` и обоснованы hook-контрактом «never crash».
- Секреты/.env/credentials в диффе: нет. `.gitignore` покрывает `.agentlog/`, `.zprof/runs/`.
- Backward compat при `audit.enabled: false`: не затронуто (CI + форматирование).

## Findings

### P2

1. **ruff не запинен** (`pip install pytest ruff`). Дефолтный rule set ruff меняется между релизами (локально 0.16.9 включает BLE/S/B/C4/DTZ/...), новый релиз может покрасить CI без изменений в коде. Предложение: `pip install 'ruff==0.16.9' pytest` или `ruff>=0.16,<0.17`.
2. **`profiles/base/telemetry_test.py` не входит в CI pytest** — CI гоняет только `profiles/base/tests/`, а cross-language contract тест для `score_defaults` лежит уровнем выше и был изменён в этом PR. Предложение: добавить путь в pytest-шаг или переместить файл в `tests/`.
3. **Комментарий в `ruff.toml`** перечисляет default rule set как «F, E, B, UP, RUF + more» — это зависит от версии ruff и неточно; в связке с P2-1 лучше явно `select` либо сослаться на запиненную версию.

### P3

1. `test -z "$(gofmt -l .)"` при падении не печатает список файлов — удобнее `out=$(gofmt -l .); echo "$out"; test -z "$out"`.
2. Коммит-префиксы `ci:` / `style(cli):` — валидный conventional commits, но вне примеров из CLAUDE.md (`feat/fix/docs/test`). Не блокер.
3. Число pytest-тестов: локально 180 vs 185 в отчёте tester'а — вероятно разница окружения/версии Python; CI на 3.10 это покажет.
4. Удалены shebang'и `#!/usr/bin/env python3` в тестовых файлах (EXE001) — ок для pytest-модулей, отмечаю для полноты.
