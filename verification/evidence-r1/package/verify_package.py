"""Verify all distributed payload hashes and original-to-public provenance."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def verify():
    manifest = json.loads((ROOT / 'MANIFEST.sha256.json').read_text())
    for name, row in manifest.items():
        path = ROOT / name
        if not path.is_file() or digest(path) != row['sha256'] or path.stat().st_size != row['bytes']:
            raise ValueError('Public manifest mismatch: ' + name)
    provenance = json.loads((ROOT / 'provenance/original-to-public.json').read_text())
    index = {row['path']: row for row in provenance}
    for name, row in index.items():
        if digest(ROOT / name) != row['public_sha256']:
            raise ValueError('Provenance/public hash mismatch: ' + name)
    original = json.loads((ROOT / 'historical/SHA256.json').read_text())
    for name, expected in original.items():
        row = index['historical/' + name]
        if row['original_sha256'] != expected:
            raise ValueError('Historical manifest/provenance mismatch: ' + name)
    redactions = json.loads((ROOT / 'provenance/redaction-log.json').read_text())
    for row in redactions:
        entry = index[row['path']]
        if any(row[k] != entry[k] for k in ('original_sha256', 'public_sha256')):
            raise ValueError('Redaction provenance mismatch')
    result = {'status': 'PASS', 'public_files_verified': len(manifest),
              'original_historical_entries_accounted_for': len(original),
              'original_to_public_entries_verified': len(index),
              'redacted_files': sum(x['public_copy'] for x in provenance),
              'redaction_replacements': sum(x['count'] for x in redactions),
              'note': 'Original hashes identify retained originals, not signatures or a claim of unchanged public bytes.'}
    return result


if __name__ == '__main__':
    print(json.dumps(verify(), indent=2))
