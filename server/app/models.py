"""Modelos ORM.

Zero-Knowledge: `payload` es JSONB opaco para el servidor; solo contiene
sobres cifrados ("sb1.…") y metadatos no sensibles (color, pinned, url…).
"""

import uuid
from datetime import datetime, timezone

from sqlalchemy import (
    JSON,
    BigInteger,
    Boolean,
    DateTime,
    ForeignKey,
    Integer,
    String,
    UniqueConstraint,
    Uuid,
    func,
)
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.orm import Mapped, mapped_column

from .db import Base

# JSON puro en tests (SQLite), JSONB nativo en PostgreSQL.
JSONVariant = JSON().with_variant(JSONB(), "postgresql")
# SQLite solo autoincrementa con INTEGER; PostgreSQL usa BIGSERIAL.
BigIntVariant = BigInteger().with_variant(Integer, "sqlite")


class User(Base):
    __tablename__ = "users"

    id: Mapped[uuid.UUID] = mapped_column(Uuid(as_uuid=True), primary_key=True, default=uuid.uuid4)
    username: Mapped[str] = mapped_column(String(64), unique=True, index=True)
    # salt del cliente (KDF) + hash Argon2id del verifier derivado en cliente.
    # El servidor NUNCA ve la contraseña de la cuenta.
    salt: Mapped[str] = mapped_column(String(64))
    verifier: Mapped[str] = mapped_column(String(256))
    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())


class VaultItem(Base):
    __tablename__ = "vault_items"
    __table_args__ = (UniqueConstraint("user_id", "item_uuid", name="uq_user_item"),)

    id: Mapped[int] = mapped_column(BigIntVariant, primary_key=True, autoincrement=True)
    user_id: Mapped[uuid.UUID] = mapped_column(
        ForeignKey("users.id", ondelete="CASCADE"), index=True
    )
    item_uuid: Mapped[str] = mapped_column(String(64), index=True)
    kind: Mapped[str] = mapped_column(String(16))  # 'note' | 'secret'
    payload: Mapped[dict] = mapped_column(JSONVariant)
    version: Mapped[int] = mapped_column(Integer, default=1)
    deleted: Mapped[bool] = mapped_column(Boolean, default=False)
    client_updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True))
    # Timestamp del SERVIDOR, asignado desde Python para consistencia
    # entre PostgreSQL y SQLite (cursor de sincronización delta).
    synced_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=lambda: datetime.now(timezone.utc),
    )
