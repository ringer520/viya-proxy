"""Refresh the embedded mainland-China allocation snapshot from APNIC.
Run in the repository root: python scripts/update-cn-ranges.py
Review the changed snapshot, then build Viya. No runtime network requests.
"""
from datetime import datetime, timezone
import ipaddress
from pathlib import Path
from urllib.request import urlopen

SOURCE = 'https://ftp.apnic.net/stats/apnic/delegated-apnic-latest'

def main():
    with urlopen(SOURCE, timeout=60) as response:
        raw = response.read(16 * 1024 * 1024).decode('utf-8')
    networks = []
    for line in raw.splitlines():
        fields = line.split('|')
        if len(fields) < 7 or fields[1] != 'CN' or fields[6] not in ('allocated', 'assigned'):
            continue
        if fields[2] == 'ipv4':
            start = ipaddress.IPv4Address(fields[3])
            networks.extend(ipaddress.summarize_address_range(start, start + int(fields[4]) - 1))
        elif fields[2] == 'ipv6':
            networks.append(ipaddress.ip_network(fields[3] + '/' + fields[4]))
    if len(networks) < 1000:
        raise RuntimeError('Incomplete APNIC snapshot; existing file was not changed')
    collapsed = []
    for version in (4, 6):
        collapsed.extend(ipaddress.collapse_addresses(n for n in networks if n.version == version))
    output = Path(__file__).resolve().parents[1] / 'cmd/viya/cn-ranges.txt'
    content = '# Source: ' + SOURCE + '\n# CN allocations; approximate geography, not a geolocation database.\n# Fetched UTC: ' + datetime.now(timezone.utc).isoformat() + '\n'
    content += '\n'.join(map(str, collapsed)) + '\n'
    temporary = output.with_suffix('.tmp')
    temporary.write_text(content, encoding='utf-8', newline='\n')
    temporary.replace(output)
    print(f'Updated {output}: {len(collapsed)} prefixes')

if __name__ == '__main__':
    main()
