#!/usr/bin/env python3

"""Authenticate GitHub Actions runner lifecycle operations with a GitHub App."""

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
            body = response.read()
            return json.loads(body) if body else {}
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


def installation_token(args: argparse.Namespace) -> str:
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
    return installation_token


def registration_token(args: argparse.Namespace) -> str:
    org = urllib.parse.quote(args.org, safe="")
    token = installation_token(args)

    runner_auth = github_request(
        args.api_url,
        "POST",
        f"/orgs/{org}/actions/runners/registration-token",
        token,
    )
    token = runner_auth.get("token")
    if not token:
        raise RuntimeError("GitHub did not return a runner registration token")
    return token


def deregister_runner(args: argparse.Namespace) -> None:
    org = urllib.parse.quote(args.org, safe="")
    token = installation_token(args)
    query = urllib.parse.urlencode({"name": args.name, "per_page": 100})
    runner_list = github_request(
        args.api_url,
        "GET",
        f"/orgs/{org}/actions/runners?{query}",
        token,
    )
    matching_runners = [
        runner
        for runner in runner_list.get("runners", [])
        if runner.get("name") == args.name
    ]

    if not matching_runners:
        print(f"GitHub runner {args.name} is already deregistered.")
        return
    if len(matching_runners) > 1:
        raise RuntimeError(
            f"GitHub returned multiple runners named {args.name}; refusing to delete"
        )

    runner_id = matching_runners[0].get("id")
    if not isinstance(runner_id, int):
        raise RuntimeError(f"GitHub did not return an ID for runner {args.name}")
    github_request(
        args.api_url,
        "DELETE",
        f"/orgs/{org}/actions/runners/{runner_id}",
        token,
    )
    print(f"Deregistered GitHub runner {args.name}.")


def add_auth_arguments(parser: argparse.ArgumentParser) -> None:
    parser.add_argument("--app-id", required=True)
    parser.add_argument("--org", required=True)
    parser.add_argument("--private-key", required=True, type=Path)
    parser.add_argument("--api-url", default="https://api.github.com")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    token_parser = subparsers.add_parser(
        "registration-token",
        help="print a new organization runner registration token",
    )
    add_auth_arguments(token_parser)
    token_parser.add_argument(
        "--check",
        action="store_true",
        help="verify authentication without printing the registration token",
    )
    deregister_parser = subparsers.add_parser(
        "deregister-runner",
        help="remove an organization runner by its exact name",
    )
    add_auth_arguments(deregister_parser)
    deregister_parser.add_argument("--name", required=True)
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
        if args.command == "deregister-runner":
            deregister_runner(args)
            return 0
    except RuntimeError as error:
        print(f"error: {error}", file=sys.stderr)
        return 1

    return 2


if __name__ == "__main__":
    raise SystemExit(main())
