from functools import lru_cache

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Configuración por variables de entorno (prefijo STRONGBOXS_)."""

    database_url: str = "postgresql+psycopg://strongboxs:strongboxs@db:5432/strongboxs"
    secret_key: str = "dev-change-me-0123456789abcdef-0123456789abcdef"  # firma JWT; sobreescribir en producción
    access_token_expire_minutes: int = 720  # 12 h
    max_payload_bytes: int = 64 * 1024
    max_items_per_push: int = 500

    model_config = SettingsConfigDict(
        env_prefix="STRONGBOXS_", env_file=".env", extra="ignore"
    )


@lru_cache
def get_settings() -> Settings:
    return Settings()
