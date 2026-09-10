#!/usr/bin/env python3

"""Mint a short-lived GitHub Actions runner registration token."""

from __future__ import annotations

import argparse
import base64
import json
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path
from typing import Any


API_VERSION = "2022-11-28"


def base64url(value: bytes) -> str:
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode("ascii")


def github_app_jwt(app_id: str, private_key: Path) -> str:
    now = int(time.time())
    issuer: int | str = int(app_id) if app_id.isdigit() else app_id
    header = {"alg": "RS256", "typ": "JWT"}
    payload = {"iat": now - 60, "exp": now + 540, "iss": issuer}

    encoded_header = base64url(
        json.dumps(header, separators=(",", ":")).encode("utf-8")
    )
    encoded_payload = base64url(
        json.dumps(payload, separators=(",", ":")).encode("utf-8")
    )
    unsigned = f"{encoded_header}.{encoded_payload}".encode("ascii")

    try:
        signature = subprocess.run(
            [
                "openssl",
                "dgst",
                "-binary",
                "-sha256",
                "-sign",
                str(private_key),
            ],
            input=unsigned,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=True,
        ).stdout
    except FileNotFoundError as error:
        raise RuntimeError("openssl is required to sign the GitHub App JWT") from error
    except subprocess.CalledProcessError as error:
        detail = error.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"could not sign the GitHub App JWT: {detail}") from error

    return f"{unsigned.decode('ascii')}.{base64url(signature)}"


def github_request(
    api_url: str,
    method: str,
    path: str,
    bearer_token: str,
) -> dict[str, Any]:
    request = urllib.request.Request(
        f"{api_url.rstrip('/')}{path}",
        data=b"{}" if method == "POST" else None,
        method=method,
        headers={
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {bearer_token}",
            "X-GitHub-Api-Version": API_VERSION,
            "User-Agent": "xtool-tart-runner",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", errors="replace")
        try:
            message = json.loads(body).get("message", body)
        except json.JSONDecodeError:
            message = body
        raise RuntimeError(
            f"GitHub API {method} {path} failed with HTTP {error.code}: {message}"
        ) from error
    except urllib.error.URLError as error:
        raise RuntimeError(f"GitHub API {method} {path} failed: {error.reason}") from error


def registration_token(args: argparse.Namespace) -> str:
    app_jwt = github_app_jwt(args.app_id, args.private_key)
    org = urllib.parse.quote(args.org, safe="")
    installation = github_request(
        args.api_url,
        "GET",
        f"/orgs/{org}/installation",
        app_jwt,
    )
    installation_id = installation.get("id")
    if not installation_id:
        raise RuntimeError("GitHub did not return an installation ID for the organization")

    installation_auth = github_request(
        args.api_url,
        "POST",
        f"/app/installations/{installation_id}/access_tokens",
        app_jwt,
    )
    installation_token = installation_auth.get("token")
    if not installation_token:
        raise RuntimeError("GitHub did not return an installation access token")

    runner_auth = github_request(
        args.api_url,
        "POST",
        f"/orgs/{org}/actions/runners/registration-token",
        installation_token,
    )
    token = runner_auth.get("token")
    if not token:
        raise RuntimeError("GitHub did not return a runner registration token")
    return token


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    token_parser = subparsers.add_parser(
        "registration-token",
        help="print a new organization runner registration token",
    )
    token_parser.add_argument("--app-id", required=True)
    token_parser.add_argument("--org", required=True)
    token_parser.add_argument("--private-key", required=True, type=Path)
    token_parser.add_argument("--api-url", default="https://api.github.com")
    token_parser.add_argument(
        "--check",
        action="store_true",
        help="verify authentication without printing the registration token",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        if args.command == "registration-token":
            token = registration_token(args)
            if args.check:
                print("GitHub App authentication succeeded.")
            else:
                print(token)
            return 0
    except RuntimeError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1

    return 2


if __name__ == "__main__":
    raise SystemExit(main())
