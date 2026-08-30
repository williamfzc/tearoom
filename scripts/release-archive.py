#!/usr/bin/env python3
"""Create a gzip tar archive with stable ownership, ordering, and timestamps."""

import gzip
import os
import sys
import tarfile


def main():
    if len(sys.argv) != 4:
        raise SystemExit("usage: release-archive.py OUTPUT STAGING SOURCE_DATE_EPOCH")
    output, staging, epoch_text = sys.argv[1:]
    epoch = int(epoch_text)
    with open(output, "wb") as stream:
        with gzip.GzipFile(fileobj=stream, mode="wb", mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as archive:
                for name in sorted(("LICENSE", "THIRD_PARTY_NOTICES", "tearoom")):
                    path = os.path.join(staging, name)
                    info = tarfile.TarInfo(name)
                    info.size = os.path.getsize(path)
                    info.mode = 0o755 if name == "tearoom" else 0o644
                    info.mtime = epoch
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    with open(path, "rb") as payload:
                        archive.addfile(info, payload)


if __name__ == "__main__":
    main()
