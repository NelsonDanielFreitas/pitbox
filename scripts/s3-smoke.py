"""Teste rápido da API S3 local (F0-03).

Primeiro os casos negativos (fail closed): sem assinatura e com chave errada têm de dar 403.
Só depois: criar bucket, PUT, GET por URL pré-assinado, e URL adulterado recusado.

Só usa a biblioteca padrão (SigV4 à mão) para não acrescentar dependências.
Lê S3_ACCESS_KEY, S3_SECRET_KEY e S3_PORT do ambiente (make s3-smoke carrega o .env).
Fala S3 genérico: nada específico do SeaweedFS (ADR 0009).
"""
import datetime
import hashlib
import hmac
import os
import sys
import urllib.error
import urllib.parse
import urllib.request

REGION = "us-east-1"
SERVICE = "s3"
HOST = f"127.0.0.1:{os.environ.get('S3_PORT', '8333')}"
ACCESS = os.environ["S3_ACCESS_KEY"]
SECRET = os.environ["S3_SECRET_KEY"]


def _hmac(key: bytes, msg: str) -> bytes:
    return hmac.new(key, msg.encode(), hashlib.sha256).digest()


def _signing_key(date: str, secret: str = "") -> bytes:
    k = _hmac(("AWS4" + (secret or SECRET)).encode(), date)
    for part in (REGION, SERVICE, "aws4_request"):
        k = _hmac(k, part)
    return k


def _canonical_query(params: dict) -> str:
    return "&".join(
        f"{urllib.parse.quote(k, safe='-_.~')}={urllib.parse.quote(v, safe='-_.~')}"
        for k, v in sorted(params.items())
    )


def signed_request(method: str, path: str, body: bytes = b"", secret: str = ""):
    now = datetime.datetime.now(datetime.timezone.utc)
    amz_date, date = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
    payload_hash = hashlib.sha256(body).hexdigest()
    headers = {"host": HOST, "x-amz-content-sha256": payload_hash, "x-amz-date": amz_date}
    signed = ";".join(sorted(headers))
    canonical = "\n".join(
        [method, path, "", "".join(f"{k}:{headers[k]}\n" for k in sorted(headers)), signed, payload_hash]
    )
    scope = f"{date}/{REGION}/{SERVICE}/aws4_request"
    to_sign = "\n".join(["AWS4-HMAC-SHA256", amz_date, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    sig = hmac.new(_signing_key(date, secret), to_sign.encode(), hashlib.sha256).hexdigest()
    auth = f"AWS4-HMAC-SHA256 Credential={ACCESS}/{scope}, SignedHeaders={signed}, Signature={sig}"
    req = urllib.request.Request(f"http://{HOST}{path}", data=body or None, method=method)
    for k, v in headers.items():
        if k != "host":
            req.add_header(k, v)
    req.add_header("Authorization", auth)
    return urllib.request.urlopen(req, timeout=10)


def presign_get(path: str, expires: int = 60) -> str:
    now = datetime.datetime.now(datetime.timezone.utc)
    amz_date, date = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
    scope = f"{date}/{REGION}/{SERVICE}/aws4_request"
    params = {
        "X-Amz-Algorithm": "AWS4-HMAC-SHA256",
        "X-Amz-Credential": f"{ACCESS}/{scope}",
        "X-Amz-Date": amz_date,
        "X-Amz-Expires": str(expires),
        "X-Amz-SignedHeaders": "host",
    }
    query = _canonical_query(params)
    canonical = "\n".join(["GET", path, query, f"host:{HOST}\n", "host", "UNSIGNED-PAYLOAD"])
    to_sign = "\n".join(["AWS4-HMAC-SHA256", amz_date, scope, hashlib.sha256(canonical.encode()).hexdigest()])
    sig = hmac.new(_signing_key(date), to_sign.encode(), hashlib.sha256).hexdigest()
    return f"http://{HOST}{path}?{query}&X-Amz-Signature={sig}"


def expect_403(label: str, fn) -> bool:
    try:
        fn()
    except urllib.error.HTTPError as e:
        if e.code == 403:
            print(f"{label}: 403 OK")
            return True
        print(f"FALHOU: {label} devolveu HTTP {e.code} (esperado 403)", file=sys.stderr)
        return False
    print(f"FALHOU: {label} foi ACEITE (S3 sem autenticação?)", file=sys.stderr)
    return False


def main() -> int:
    bucket = f"smoke-{os.urandom(4).hex()}"
    key, data = "hello.txt", b"pitbox s3 smoke test"
    if not (
        expect_403("pedido sem assinatura", lambda: urllib.request.urlopen(f"http://{HOST}/", timeout=10))
        and expect_403("chave errada", lambda: signed_request("GET", "/", secret="x" * 40))
    ):
        return 1
    try:
        signed_request("PUT", f"/{bucket}")
        print(f"bucket criado: {bucket}")
        signed_request("PUT", f"/{bucket}/{key}", data)
        print(f"upload: {key} ({len(data)} bytes)")
        with urllib.request.urlopen(presign_get(f"/{bucket}/{key}"), timeout=10) as r:  # sem credenciais no pedido
            got = r.read()
        if got != data:
            print("FALHOU: conteúdo diferente", file=sys.stderr)
            return 1
        print("download por URL pré-assinado: OK")
        # Um URL com assinatura adulterada tem de ser recusado (fail closed).
        bad = presign_get(f"/{bucket}/{key}")[:-4] + "0000"
        try:
            urllib.request.urlopen(bad, timeout=10)
            print("FALHOU: URL adulterado foi aceite", file=sys.stderr)
            return 1
        except urllib.error.HTTPError as e:
            print(f"URL adulterado recusado (HTTP {e.code}): OK")
        return 0
    except (urllib.error.URLError, OSError) as e:
        print(f"FALHOU: {e}", file=sys.stderr)
        return 1
    finally:
        for method, path in (("DELETE", f"/{bucket}/{key}"), ("DELETE", f"/{bucket}")):
            try:
                signed_request(method, path)
            except Exception:
                pass


if __name__ == "__main__":
    sys.exit(main())
