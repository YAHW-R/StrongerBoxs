"""Esquemas Pydantic con validación Zero-Knowledge.

Regla de oro: cualquier campo sensible DEBE llegar como sobre cifrado
("sb1.<base64url>") o como cadena vacía. Si llega texto claro, la API
rechaza la petición: así el servidor garantiza que jamás almacena
contenido legible.
"""

import re
from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field, model_validator

ENVELOPE_RE = re.compile(r"^sb1\.[A-Za-z0-9_-]+$")
USERNAME_RE = r"^[a-z0-9_.-]{3,64}$"
COLOR_RE = re.compile(r"^#[0-9a-fA-F]{6}$")

SENSITIVE_FIELDS: dict[str, tuple[str, ...]] = {
    "note": ("title", "body"),
    "secret": ("title", "username", "password", "notes"),
}

ALLOWED_KEYS: dict[str, set[str]] = {
    "note": {"title", "body", "color", "pinned", "archived"},
    "secret": {"title", "username", "password", "url", "notes"},
}


class SaltRequest(BaseModel):
    username: str = Field(pattern=USERNAME_RE)


class SaltResponse(BaseModel):
    salt: str


class VerifierAuth(BaseModel):
    username: str = Field(pattern=USERNAME_RE)
    # hex(SHA256) del KDF Argon2id calculado EN el cliente.
    verifier: str = Field(pattern=r"^[0-9a-f]{64}$")


class RegisterRequest(VerifierAuth):
    salt: str = Field(min_length=16, max_length=64)


class TokenResponse(BaseModel):
    access_token: str
    token_type: str = "bearer"
    expires_in: int
    user_id: str


class ItemIn(BaseModel):
    item_uuid: str = Field(min_length=8, max_length=64)
    kind: Literal["note", "secret"]
    payload: dict[str, Any]
    version: int = Field(ge=1)
    deleted: bool = False
    updated_at: datetime

    @model_validator(mode="after")
    def validate_zk_payload(self) -> "ItemIn":
        allowed = ALLOWED_KEYS[self.kind]
        sensitive = SENSITIVE_FIELDS[self.kind]

        extra = set(self.payload) - allowed
        if extra:
            raise ValueError(f"claves no permitidas para '{self.kind}': {sorted(extra)}")

        for key, value in self.payload.items():
            if key in sensitive:
                if not isinstance(value, str):
                    raise ValueError(f"'{key}' debe ser cadena")
                if value and not ENVELOPE_RE.match(value):
                    raise ValueError(f"'{key}' debe ir cifrado (sb1.*) o vacío: Zero-Knowledge")
            else:
                if not isinstance(value, (str, bool, int)) or value is None:
                    raise ValueError(f"metadato '{key}' debe ser escalar")
                if key == "color" and isinstance(value, str) and value and not COLOR_RE.match(value):
                    raise ValueError("color inválido (esperado #RRGGBB)")
                if key == "url" and isinstance(value, str) and len(value) > 256:
                    raise ValueError("url demasiado larga")
        return self


class PushRequest(BaseModel):
    items: list[ItemIn] = Field(min_length=1, max_length=500)


class ItemResult(BaseModel):
    item_uuid: str
    status: Literal["accepted", "skipped"]
    reason: str | None = None
    server_version: int | None = None


class PushResponse(BaseModel):
    accepted: list[ItemResult]
    skipped: list[ItemResult]


class PullRequest(BaseModel):
    since: datetime | None = None


class ItemOut(BaseModel):
    item_uuid: str
    kind: str
    payload: dict[str, Any]
    version: int
    deleted: bool
    updated_at: datetime
    synced_at: datetime


class PullResponse(BaseModel):
    items: list[ItemOut]
    server_time: datetime
