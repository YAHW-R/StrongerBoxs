from contextlib import asynccontextmanager

from fastapi import FastAPI

from .config import get_settings
from .db import Base, get_engine
from .routers import auth, items


@asynccontextmanager
async def lifespan(app: FastAPI):
    Base.metadata.create_all(bind=get_engine())
    yield


def create_app() -> FastAPI:
    settings = get_settings()
    app = FastAPI(
        title="Strongboxs API",
        version="0.1.0",
        description="Sincronización Zero-Knowledge: el servidor solo almacena ciphertext.",
        lifespan=lifespan,
    )
    app.include_router(auth.router)
    app.include_router(items.router)

    @app.get("/health", tags=["ops"])
    def health():
        return {"status": "ok"}

    return app


app = create_app()
