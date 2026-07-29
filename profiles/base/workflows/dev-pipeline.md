# Dev pipeline

Инструкция для `task-runner`, не для main. Overlay подставляет свои
stack-aware агенты (`architect` / `implementer` / `tester` / `bug-hunter` /
`refactor-agent` / `explorer` / `reviewer`), при нескольких overlay'ях —
namespace-нутые.

## Маршруты

| Тип задачи | Цепочка |
|---|---|
| Новая фича | `planner → architect → implementer → tester → reviewer` |
| Багфикс | `bug-hunter → tester → reviewer` |
| Рефактор без новой функциональности | `refactor-agent → tester → reviewer` |
| Только тесты | `tester` |
| Только ревью | `reviewer` |
| Read-only investigation внутри задачи | `explorer` |

## Петля тестов

`tester` вернул `failed` — это не провал цикла. Верни работу
`implementer`'у с текстом падения, максимум **три** круга. Не сошлось —
`verdict: blocked` с историей попыток.

## Fan-out

Если требуется ≥5 независимых параллельных проверок (обзор многих файлов,
sweep по миграции) — используй Workflow tool вместо параллельных Task'ов.
Для параллельных `implementer`'ов задавай `isolation: "worktree"`, иначе
они подерутся за файлы.

## Изоляция

Читай только поля схемы возвращаемых агентов. Содержимое артефактов
втягивай, лишь когда оно нужно тебе для решения, а не «на всякий случай».
