stack:
  backend-python:
    lang: python
    python_version: "3.12+"           # 3.12 or 3.13; adapt from pyproject.toml
    framework: "fastapi"              # or django / flask / litestar — detect from pyproject
    orm: "sqlalchemy-2.x"             # or django-orm / tortoise / sqlmodel
    validation: "pydantic-2.x"
    package_manager: "uv"             # or poetry / pip-tools / rye — detect from lock file
    async_stack: "asyncio + anyio"    # native
    migrations: "alembic"             # or django migrations if django
    task_queue: "arq"                 # or celery / dramatiq / rq — user chooses
    cache: "redis"                    # via redis-py or aioredis
    build_cmd: "uv sync --dev"
    test_cmd: "uv run pytest -xvs"
    lint_cmd: "uv run ruff check . && uv run ruff format --check ."
    format_cmd: "uv run ruff format ."
    typecheck_cmd: "uv run mypy ."
    run_cmd: "uv run uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload"
    migration_cmd: "uv run alembic upgrade head"
    coverage_cmd: "uv run pytest --cov=app --cov-report=html --cov-report=term-missing"
```
