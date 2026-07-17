## Reverse Engineering / Mach-O loop extension

Расширение exploratory workflow для reverse engineering Mach-O binaries (macOS + iOS).

### Trigger phrases
- EN: `reverse-engineer <binary>`, `unpack <ipa>`, `dump classes`, `find function <name>`, `trace calls`, `audit entitlements`
- RU: `разбери бинарь`, `распакуй ipa`, `дампни классы`, `найди функцию`, `трейс вызовов`, `аудит энтайтлментов`

### Pipeline (exploratory)
Стандартный exploratory-workflow: `intake → unpack → explore → hypothesize → verify → report`.

Каждая стадия ↔ role:
1. **intake** — принимает бинарь, определяет scope, авторизацию, целевые вопросы
2. **unpacker** — распаковывает `.ipa`/`.app`/`.dmg`/`.pkg`, thin fat binary via `lipo`, декрипт (если jailbreak)
3. **explorer** — читает Mach-O headers, load commands, symbols, entitlements, code signature; выделяет структуру
4. **hypothesizer** — формулирует гипотезы о работе; выбирает точки инструментирования (Frida hooks, lldb breakpoints)
5. **verifier** — валидирует гипотезы через lldb / Frida / dtrace; собирает evidence
6. **report-writer** — финальный markdown-отчёт в `reports/<binary-slug>-<YYYY-MM-DD>.md`

### Специальные диспатчи
| Задача | Агент |
|---|---|
| Static headers / symbols / dependencies | `otool-runner` |
| Objective-C / Swift class dump | `class-dump-runner` |
| Дебаг attach на живой процесс | `lldb-attach` |
| Runtime hooks на функции / методы | `frida-instrumentor` |
| Открыть в интерактивном disassembler | `hopper-launcher` |
| Проверить codesign + entitlements | `entitlements-parser` |

### Изоляция — специфичные правила
- **NEVER modify target binary** — RE is read-only; ре-sign через `codesign --force` — только для образовательных копий, не оригинал.
- **NEVER attach lldb / Frida к prod-процессам** без explicit approval + backup: убивает работающее приложение.
- **NEVER анализировать бинари без legal authorization** — respect EULA + local law; уточнить scope у intake.
- **Binary size limits** — .app bundle может быть 500MB+; отдельные dylib 10-50MB. Никогда не читать бинарь как text; использовать `strings`, `otool`, `nm`, `class-dump` для structured extraction.
- **class-dump / dsdump output** может быть 50k+ строк. Filter по интересующему классу/framework, не dump'ать целиком в контекст.
- **Frida script output** может флудить (thousands of events/sec). Всегда сохранять в `/tmp/frida-<ts>.log`, парсить offline.
- **lldb backtrace на сложном ObjC/Swift stack** — 100+ frames. Возвращать только first N frames + Swift-demangled.
- **NEVER коммитить distributable binaries из `unpacked/` в git** — обычно нарушение лицензии + гит будет тяжёлый.
- **Output — markdown reports в `reports/<slug>-<YYYY-MM-DD>.md`**, НЕ PR, НЕ коммиты кода.
