from sqlalchemy import create_engine
from sqlalchemy.orm import DeclarativeBase, sessionmaker

from .config import get_settings

settings = get_settings()

_engine = None


def get_engine():
    """Motor perezoso: no se construye hasta usarse (los tests lo evitan)."""
    global _engine
    if _engine is None:
        _engine = create_engine(settings.database_url, pool_pre_ping=True)
    return _engine


class Base(DeclarativeBase):
    pass


def session_factory():
    return sessionmaker(bind=get_engine(), autoflush=False, expire_on_commit=False)


def get_db():
    db = session_factory()()
    try:
        yield db
    finally:
        db.close()
