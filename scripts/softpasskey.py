#!/usr/bin/env python3
"""Passkey de software para os smokes (#158): regista uma passkey "none attestation" numa conta de teste.

Só local, só para provar o fluxo de recuperação ponta a ponta sem browser nem dispositivo. Usa o
openssl para a chave P-256 (como o DeviceKey do auth-smoke) e um codificador CBOR mínimo (só os tipos
que o WebAuthn precisa aqui). Só biblioteca padrão.
"""
import base64
import hashlib
import json
import os
import subprocess
import tempfile


def b64u(raw: bytes) -> str:
    return base64.urlsafe_b64encode(raw).decode().rstrip("=")


def b64u_decode(text: str) -> bytes:
    return base64.urlsafe_b64decode(text + "=" * (-len(text) % 4))


def _head(major: int, n: int) -> bytes:
    if n < 24:
        return bytes([major << 5 | n])
    if n < 1 << 8:
        return bytes([major << 5 | 24, n])
    if n < 1 << 16:
        return bytes([major << 5 | 25]) + n.to_bytes(2, "big")
    return bytes([major << 5 | 26]) + n.to_bytes(4, "big")


def cbor(value) -> bytes:
    """CBOR (RFC 8949) para int, bytes, str e dict (chaves int ou str, por ordem de inserção)."""
    if isinstance(value, bool) or value is None:
        raise TypeError("não precisamos de bool nem null")
    if isinstance(value, int):
        return _head(0, value) if value >= 0 else _head(1, -1 - value)
    if isinstance(value, bytes):
        return _head(2, len(value)) + value
    if isinstance(value, str):
        raw = value.encode()
        return _head(3, len(raw)) + raw
    if isinstance(value, dict):
        return _head(5, len(value)) + b"".join(cbor(k) + cbor(v) for k, v in value.items())
    raise TypeError(f"tipo CBOR não suportado: {type(value)}")


class SoftPasskey:
    """Uma credencial WebAuthn ES256 na memória (chave no disco temporário, apagada no fim)."""

    def __init__(self):
        self._dir = tempfile.mkdtemp(prefix="pitbox-passkey-")
        self.pem = os.path.join(self._dir, "key.pem")
        subprocess.run(["openssl", "ecparam", "-genkey", "-name", "prime256v1", "-noout", "-out", self.pem],
                       check=True, capture_output=True)
        der = subprocess.run(["openssl", "pkey", "-in", self.pem, "-pubout", "-outform", "DER"],
                             check=True, capture_output=True).stdout
        point = der[-65:]  # SPKI P-256: os últimos 65 bytes são 04 || x || y
        if point[0] != 4:
            raise RuntimeError("chave pública inesperada")
        self.x, self.y = point[1:33], point[33:]
        self.credential_id = os.urandom(32)

    def close(self):
        for name in os.listdir(self._dir):
            os.unlink(os.path.join(self._dir, name))
        os.rmdir(self._dir)

    def registration(self, options: dict, origin: str) -> dict:
        """Responde a publicKeyCredentialCreationOptions do Zitadel com uma attestation "none"."""
        public_key = options["publicKey"]
        rp_id = public_key["rp"]["id"]
        client_data = json.dumps({"type": "webauthn.create", "challenge": public_key["challenge"],
                                  "origin": origin, "crossOrigin": False}).encode()
        cose = cbor({1: 2, 3: -7, -1: 1, -2: self.x, -3: self.y})
        # flags: UP (1) | UV (4) | AT (64)
        auth_data = (hashlib.sha256(rp_id.encode()).digest() + bytes([0x45]) + (0).to_bytes(4, "big")
                     + bytes(16) + len(self.credential_id).to_bytes(2, "big") + self.credential_id + cose)
        attestation = cbor({"fmt": "none", "attStmt": {}, "authData": auth_data})
        return {"id": b64u(self.credential_id), "rawId": b64u(self.credential_id), "type": "public-key",
                "response": {"attestationObject": b64u(attestation), "clientDataJSON": b64u(client_data)}}
