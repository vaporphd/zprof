## Backend / Python (FastAPI) section (для PROJECT_SPEC.md)

### Runtime
- Python: <e.g. 3.12.7> (from `.python-version` or pyproject `requires-python`)
- Framework: FastAPI <version> / Django / Flask / Litestar
- ASGI server: Uvicorn <version> (dev), Gunicorn + Uvicorn worker (prod)
- Worker count: <e.g. `2 * $CPU_COUNT + 1`>

### Data
- ORM: SQLAlchemy 2.x (async) / Django ORM / Tortoise / SQLModel
- Database: PostgreSQL <version> / MySQL / SQLite (dev only)
- Connection pool: <e.g. asyncpg pool 5-20>
- Migrations: Alembic <version> — path `alembic/`
- Cache: Redis <version> — via redis-py async

### Validation / Serialization
- Pydantic 2.x — models under `app/schemas/`
- Settings via `pydantic-settings` — `app/config.py`; secrets as `SecretStr`
- Response serialization: FastAPI's `response_model=`

### Async
- Native asyncio + `anyio` for portable I/O
- Structured concurrency: `asyncio.TaskGroup` (3.11+) or `anyio.create_task_group`
- Background tasks: FastAPI `BackgroundTasks` for short; `arq`/`celery` for long

### Testing
- Framework: pytest <version> + pytest-asyncio (auto mode)
- HTTP client: `httpx.AsyncClient(transport=ASGITransport(app=app))`
- Fixtures: factory-boy or model_bakery for data; per-test transaction rollback via SQLAlchemy `SAVEPOINT`
- Coverage: pytest-cov, target `>= 80%`
- Property-based: hypothesis (optional)

### Tooling
- Package manager: uv <version> (recommended) / poetry / pip-tools / rye
- Linter+formatter: ruff <version> — configured in `pyproject.toml` `[tool.ruff]`
- Type checker: mypy <version> in strict mode — `[tool.mypy]` `strict = true`
- Pre-commit: pre-commit <version> with ruff + mypy + trailing-whitespace hooks

### Deployment
- Container: Docker (multi-stage build: uv sync → copy → uvicorn)
- Orchestration: Kubernetes / ECS / Fly.io / Railway / VPS + systemd
- Health probes: `/health` (liveness), `/ready` (readiness — DB + Redis reachable)
- Observability: OpenTelemetry SDK → OTLP → Grafana Tempo / Jaeger; logs to stdout as JSON

### CI
- GitHub Actions / GitLab CI / Buildkite / self-hosted
- Steps: uv sync → ruff check → mypy → pytest --cov → build image → push
- Service containers: postgres:16, redis:7

### Known caveats
- <e.g. Uvicorn worker recycled every N requests to avoid memory bloat>
- <e.g. Prod Postgres requires `?ssl=require` in DATABASE_URL>
