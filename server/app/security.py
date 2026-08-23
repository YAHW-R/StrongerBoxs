"""Seguridad: hashing de verifier (Argon2id) y JWT.

El servidor jamás ve la contraseña de la cuenta: el cliente deriva
`verifier = hex(SHA256(Argon2id(password, salt)))` y aquí solo se
almacena/compara un hash Argon2id de ese valor de alta entropía.
"""

import hashlib
import hmac
import uuid
from datetime import datetime, timedelta, timezone

import jwt
from argon2 import PasswordHasher
from argon2.exceptions import InvalidHashError, VerificationError, VerifyMismatchError
from fastapi import Depends, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from sqlalchemy.orm import Session

from .config import get_settings
from .db import get_db
from .models import User

settings = get_settings()
_ph = PasswordHasher()
_bearer = HTTPBearer(auto_error=False)


def dummy_salt(username: str) -> str:
    """Salt determinista para usuarios inexistentes (evita enumeración)."""
    digest = hmac.new(
        settings.secret_key.encode(), f"dummy:{username.lower()}".encode(), hashlib.sha256
    ).hexdigest()
    return digest[:32]


def hash_verifier(verifier: str) -> str:
    return _ph.hash(verifier)


def verify_verifier(stored_hash: str, verifier: str) -> bool:
    try:
        return _ph.verify(stored_hash, verifier)
    except (VerifyMismatchError, VerificationError, InvalidHashError):
        return False


def create_access_token(user_id: uuid.UUID) -> tuple[str, int]:
    expires_in = settings.access_token_expire_minutes * 60
    payload = {
        "sub": str(user_id),
        "exp": datetime.now(timezone.utc) + timedelta(seconds=expires_in),
    }
    token = jwt.encode(payload, settings.secret_key, algorithm="HS256")
    return token, expires_in


def get_current_user(
    credentials: HTTPAuthorizationCredentials | None = Depends(_bearer),
    db: Session = Depends(get_db),
) -> User:
    if credentials is None or credentials.scheme.lower() != "bearer":
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Falta el token Bearer")

    try:
        payload = jwt.decode(credentials.credentials, settings.secret_key, algorithms=["HS256"])
        user_id = uuid.UUID(payload["sub"])
    except (jwt.PyJWTError, KeyError, ValueError):
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Token inválido o expirado")

    user = db.get(User, user_id)
    if user is None:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Usuario no encontrado")
    return user
