from fastapi import APIRouter, Depends, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import Session

from ..db import get_db
from ..models import User
from ..schemas import RegisterRequest, SaltRequest, SaltResponse, TokenResponse, VerifierAuth
from ..security import (
    create_access_token,
    dummy_salt,
    hash_verifier,
    verify_verifier,
)

router = APIRouter(prefix="/auth", tags=["auth"])


@router.post("/salt", response_model=SaltResponse)
def get_salt(body: SaltRequest, db: Session = Depends(get_db)) -> SaltResponse:
    """Entrega el salt KDF del usuario. Para usuarios inexistentes devuelve
    un salt determinista falso (misma respuesta ⇒ no se puede enumerar)."""
    user = db.execute(select(User).where(User.username == body.username)).scalar_one_or_none()
    return SaltResponse(salt=user.salt if user else dummy_salt(body.username))


@router.post("/register", response_model=TokenResponse, status_code=status.HTTP_201_CREATED)
def register(body: RegisterRequest, db: Session = Depends(get_db)) -> TokenResponse:
    username = body.username.lower()
    exists = db.execute(select(User).where(User.username == username)).scalar_one_or_none()
    if exists is not None:
        raise HTTPException(status.HTTP_409_CONFLICT, "El usuario ya existe")

    user = User(username=username, salt=body.salt, verifier=hash_verifier(body.verifier))
    db.add(user)
    db.commit()

    token, expires_in = create_access_token(user.id)
    return TokenResponse(access_token=token, expires_in=expires_in, user_id=str(user.id))


@router.post("/login", response_model=TokenResponse)
def login(body: VerifierAuth, db: Session = Depends(get_db)) -> TokenResponse:
    user = db.execute(
        select(User).where(User.username == body.username.lower())
    ).scalar_one_or_none()
    if user is None or not verify_verifier(user.verifier, body.verifier):
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Credenciales inválidas")

    token, expires_in = create_access_token(user.id)
    return TokenResponse(access_token=token, expires_in=expires_in, user_id=str(user.id))
