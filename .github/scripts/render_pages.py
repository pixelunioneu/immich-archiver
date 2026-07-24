#!/usr/bin/env python3
"""Regenerate docs/index.html from the repo's GitHub Releases.

Run by .github/workflows/release.yml after GoReleaser publishes a release.
Requires GITHUB_TOKEN and REPO ("owner/name") in the environment.
"""
import html
import json
import os
import urllib.request

REPO = os.environ["REPO"]
TOKEN = os.environ["GITHUB_TOKEN"]

PLATFORM_LABELS = [
    ("linux_amd64", "Linux (x86_64)"),
    ("linux_arm64", "Linux (ARM64)"),
    ("darwin_amd64", "macOS (Intel)"),
    ("darwin_arm64", "macOS (Apple Silicon)"),
    ("windows_amd64", "Windows (x86_64)"),
]


def fetch_releases():
    req = urllib.request.Request(
        f"https://api.github.com/repos/{REPO}/releases",
        headers={
            "Authorization": f"Bearer {TOKEN}",
            "Accept": "application/vnd.github+json",
        },
    )
    with urllib.request.urlopen(req) as resp:
        return json.load(resp)


def asset_for(assets, platform_key):
    for a in assets:
        if platform_key in a["name"]:
            return a
    return None


def render(releases):
    releases = [r for r in releases if not r.get("draft")]
    latest = releases[0] if releases else None

    rows = []
    if latest:
        for key, label in PLATFORM_LABELS:
            a = asset_for(latest["assets"], key)
            if a:
                rows.append(
                    f'<li><a href="{html.escape(a["browser_download_url"])}">{html.escape(label)}</a> '
                    f'<span class="size">({a["size"] // 1024 // 1024} MB)</span></li>'
                )

    history_items = "".join(
        f'<li><a href="{html.escape(r["html_url"])}">{html.escape(r["tag_name"])}</a> '
        f'&mdash; {html.escape(r["published_at"] or "")}</li>'
        for r in releases[1:11]
    )

    latest_version = html.escape(latest["tag_name"]) if latest else "unreleased"
    latest_url = html.escape(latest["html_url"]) if latest else "#"
    downloads = "\n      ".join(rows) if rows else "<li>No release assets found.</li>"

    return f"""<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>immich-archiver</title>
  <style>
    body {{ font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem; color: #1a1a1a; }}
    h1 {{ margin-bottom: 0.25rem; }}
    .subtitle {{ color: #666; margin-top: 0; }}
    ul {{ list-style: none; padding: 0; }}
    li {{ margin: 0.5rem 0; }}
    .size {{ color: #888; font-size: 0.9em; }}
    a {{ color: #2563eb; text-decoration: none; }}
    a:hover {{ text-decoration: underline; }}
    section {{ margin-top: 2.5rem; }}
    @media (prefers-color-scheme: dark) {{
      body {{ color: #eee; background: #111; }}
      .subtitle {{ color: #aaa; }}
      .size {{ color: #999; }}
      a {{ color: #60a5fa; }}
    }}
  </style>
</head>
<body>
  <h1>immich-archiver</h1>
  <p class="subtitle">Mirror an Immich instance's timeline onto local disk.</p>

  <section>
    <h2>Download latest (<a href="{latest_url}">{latest_version}</a>)</h2>
    <ul>
      {downloads}
    </ul>
  </section>

  <section>
    <h2>Previous releases</h2>
    <ul>{history_items}</ul>
    <p><a href="https://github.com/{html.escape(REPO)}/releases">All releases on GitHub</a></p>
  </section>
</body>
</html>
"""


def main():
    releases = fetch_releases()
    os.makedirs("docs", exist_ok=True)
    with open("docs/index.html", "w") as f:
        f.write(render(releases))


if __name__ == "__main__":
    main()
