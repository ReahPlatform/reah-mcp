"""Keep the Go signer on the same reviewed approval schema as the Node client."""
import base64
from pathlib import Path
import re

root = Path(__file__).resolve().parents[1]
source = (root / "src/generated/approval_details_pb.ts").read_text()
encoded = re.search(r'fileDesc\("([A-Za-z0-9+/=]+)"', source).group(1)
expected = base64.b64decode(encoded + "=" * (-len(encoded) % 4))
actual = (root / "internal/client/approval.descriptor.bin").read_bytes()
if actual != expected:
    raise SystemExit("Go approval descriptor differs from the pinned TypeScript schema")
print("Go and TypeScript approval descriptors match.")
