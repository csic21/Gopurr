#!/usr/bin/env python3
"""Collect bundled Rust dependencies' license texts from locked Cargo metadata."""
import argparse
import base64
import re
import subprocess
from urllib.parse import urlparse
import json
from pathlib import Path
import shutil

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--metadata', type=Path, required=True)
parser.add_argument('--out', type=Path, required=True)
args = parser.parse_args()
metadata = json.loads(args.metadata.read_text(encoding='utf-8-sig'))
packages = {p['id']: p for p in metadata['packages']}
nodes = {n['id']: n for n in metadata['resolve']['nodes']}
pending = [metadata['resolve']['root']]
selected = set()
while pending:
    identity = pending.pop()
    if identity in selected:
        continue
    selected.add(identity)
    pending.extend(d['pkg'] for d in nodes[identity]['deps'])
args.out.mkdir(parents=True, exist_ok=True)
cache = Path('.cache/rust-license-texts')

def upstream_license_files(package, source):
    # Some crates omit their repository-level license from the crates.io tar.
    # Recover it from the exact source commit recorded in that tar, never from
    # a mutable default branch or an unrelated SPDX template.
    vcs_file = source / '.cargo_vcs_info.json'
    repository = urlparse(package.get('repository') or '')
    if repository.hostname != 'github.com' or not vcs_file.is_file():
        return []
    vcs = json.loads(vcs_file.read_text())
    commit = vcs.get('git', {}).get('sha1', '')
    parts = repository.path.strip('/').removesuffix('.git').split('/')
    if len(parts) != 2 or not re.fullmatch(r'[0-9a-f]{40}', commit) or not all(re.fullmatch(r'[A-Za-z0-9_.-]+', v) for v in parts):
        return []
    directory = cache / ('-'.join(parts) + '-' + commit)
    if not (directory / '.complete').is_file():
        directory.mkdir(parents=True, exist_ok=True)
        repo = '/'.join(parts)
        tree = subprocess.run(['gh','api',f'repos/{repo}/git/trees/{commit}'], check=True, capture_output=True, text=True)
        for entry in json.loads(tree.stdout)['tree']:
            name = entry['path']
            if entry['type'] != 'blob' or not re.fullmatch(r'(LICENSE|LICENCE|COPYING|NOTICE)([-_.][A-Za-z0-9_.-]+)?', name.upper()):
                continue
            sha = entry['sha']
            if not re.fullmatch(r'[0-9a-f]{40}', sha) or entry.get('size', 0) > 2 << 20:
                raise SystemExit(f'Invalid upstream license blob for {repo}')
            blob = subprocess.run(['gh','api',f'repos/{repo}/git/blobs/{sha}'], check=True, capture_output=True, text=True)
            data = json.loads(blob.stdout)
            if data['encoding'] != 'base64':
                raise SystemExit(f'Unexpected license encoding for {repo}')
            (directory / name).write_bytes(base64.b64decode(data['content']))
        (directory / '.complete').write_text(commit)
    return sorted(p for p in directory.iterdir() if p.is_file() and p.name != '.complete')

index = ['# Bundled Rust dependency notices', '', 'Generated from the checked-in Windows Cargo.lock and source license texts.', '']
for identity in sorted(selected):
    package = packages[identity]
    source = Path(package['manifest_path']).parent
    files = []
    license_file = package.get('license_file')
    if license_file:
        files.append(source / license_file)
    # NSH's workspace members inherit the repository's dual license. Stop at
    # the first ancestor containing license texts; do not invent substitutes.
    for directory in [source, *list(source.parents)[:5]]:
        found = sorted(p for p in directory.iterdir() if p.is_file() and p.name.upper().startswith(('LICENSE', 'LICENCE', 'COPYING', 'NOTICE')) and p.suffix.lower() not in ('.rs', '.go', '.py'))
        if found:
            files.extend(found)
            break
    # Vendored engines and fonts can carry additional notices below the
    # crate root (for example QuickJS's own copyright and license).
    nested = [p for p in source.rglob('*') if p.is_file() and not p.is_symlink() and re.fullmatch(r'(LICENSE|LICENCE|COPYING|NOTICE)([-_.][A-Za-z0-9_.-]+)?', p.name.upper())]
    if not files:
        files = upstream_license_files(package, source)
    if not files and package['name'] == 'mac' and package['version'] == '0.1.1':
        # This historical crate declares MIT/Apache-2.0 in both its manifest
        # and README, but publishes no standalone text. Select Apache-2.0
        # and retain the author's original declaration and attribution.
        files = [source / 'Cargo.toml', source / 'README.md', Path('licenses/nsh-apache-2.0.txt')]
    if not files and package.get('license') == 'CC0-1.0':
        # Preserve the author's public-domain declaration from the published
        # crate when upstream supplies no standalone license document.
        files = [source / 'Cargo.toml']
    if not files:
        raise SystemExit(f"Missing source license texts for {package['name']} {package['version']}")
    name = f"{package['name']}-{package['version']}"
    destination = args.out / name
    destination.mkdir(exist_ok=True)
    for path in set(files + nested):
        relative = path.relative_to(source) if path.is_relative_to(source) else Path(path.name)
        target = destination / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(path, target)
    index.extend([f"## {name}", '', package.get('license') or 'See the included upstream license texts.', '', package.get('repository') or package.get('homepage') or f'https://crates.io/crates/{package["name"]}/{package["version"]}', ''])
(args.out / 'INDEX.md').write_text('\n'.join(index), encoding='utf-8')
print(f'Collected source license texts for {len(selected)} locked Rust packages.')
