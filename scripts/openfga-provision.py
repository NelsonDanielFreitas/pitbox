#!/usr/bin/env python3
"""Cria a store e o modelo OpenFGA no ambiente local e guarda os ids no .env.

É idempotente: se PITBOX_OPENFGA_STORE_ID e PITBOX_OPENFGA_MODEL_ID já existirem,
não faz nada. Correr depois de `make up`.
"""

import json
import os
import re
import sys
from pathlib import Path

import urllib.request

ROOT = Path(__file__).resolve().parent.parent
ENV_FILE = ROOT / "deploy" / "compose" / ".env"
MODEL_FILE = ROOT / "backend" / "authz" / "model.json"


def get(key: str, default: str = "") -> str:
    if not ENV_FILE.exists():
        return default
    text = ENV_FILE.read_text(encoding="utf-8")
    m = re.search(rf"^{re.escape(key)}=(.*)$", text, re.MULTILINE)
    return m.group(1).strip() if m else default


def set_or_replace(key: str, value: str) -> None:
    text = ENV_FILE.read_text(encoding="utf-8") if ENV_FILE.exists() else ""
    line = f"{key}={value}\n"
    if re.search(rf"^{re.escape(key)}=.*$", text, re.MULTILINE):
        text = re.sub(rf"^{re.escape(key)}=.*$", line.rstrip(), text, flags=re.MULTILINE)
    else:
        text += line
    ENV_FILE.write_text(text, encoding="utf-8")


def api_request(method: str, path: str, body: dict | None = None) -> dict:
    port = get("OPENFGA_HTTP_PORT", "8090")
    psk = get("OPENFGA_PRESHARED_KEY")
    url = f"http://127.0.0.1:{port}{path}"
    data = json.dumps(body).encode() if body else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    if psk:
        req.add_header("Authorization", f"Bearer {psk}")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return json.loads(resp.read().decode())
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode()
        raise SystemExit(f"OpenFGA request failed: {exc.code} {detail}") from exc
    except Exception as exc:
        raise SystemExit(f"Cannot reach OpenFGA at {url}: {exc}") from exc


def main() -> int:
    if not ENV_FILE.exists():
        print(f"Missing {ENV_FILE}: run 'make init' first", file=sys.stderr)
        return 1

    store_id = get("PITBOX_OPENFGA_STORE_ID")
    model_id = get("PITBOX_OPENFGA_MODEL_ID")
    if store_id and model_id:
        print(f"OpenFGA already provisioned: store={store_id} model={model_id}")
        return 0

    print("Creating OpenFGA store...")
    store = api_request("POST", "/stores", {"name": "pitbox"})
    store_id = store["id"]

    print("Writing PitBox authorization model...")
    model = json.loads(MODEL_FILE.read_text(encoding="utf-8"))
    written = api_request("POST", f"/stores/{store_id}/authorization-models", model)
    model_id = written["authorization_model_id"]

    port = get("OPENFGA_HTTP_PORT", "8090")
    set_or_replace("PITBOX_OPENFGA_ADDR", f"127.0.0.1:{port}")
    set_or_replace("PITBOX_OPENFGA_PRESHARED_KEY", get("OPENFGA_PRESHARED_KEY"))
    set_or_replace("PITBOX_OPENFGA_STORE_ID", store_id)
    set_or_replace("PITBOX_OPENFGA_MODEL_ID", model_id)

    print(f"OpenFGA provisioned: store={store_id} model={model_id}")
    print(f"Updated {ENV_FILE}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
