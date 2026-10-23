#!/usr/bin/env python3
"""Build and sign the explicit ikesu Mac-dialog release, restoring keychain state."""
import base64
import hashlib
import os
from pathlib import Path
import platform
import plistlib
import re
import secrets
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile

IDENTITY = "Developer ID Application: KAZUHIRO HOMMA (67M8FDFZ28)"
REQUIREMENT = ('=anchor apple generic and certificate leaf[subject.OU] = "67M8FDFZ28" '
               'and certificate leaf[subject.CN] = "' + IDENTITY + '"')


def stage_release_files(repository, package):
    sources = repository / "apps/mac-dialog-decisions"
    for source in sources.glob("*.mjs"):
        shutil.copy2(source, package / source.name)
    for name in ("ensure-daemon", "ikesu-plugin.toml"):
        shutil.copy2(sources / name, package / name)
    shutil.copytree(repository / "third_party/unicode-width", package / "third_party/unicode-width")


def main():
    if len(sys.argv) != 3 or not re.fullmatch(r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)", sys.argv[1]):
        raise RuntimeError("usage: package_mac_dialog.py MAJOR.MINOR.PATCH OUTPUT")
    version, output = sys.argv[1], Path(sys.argv[2]).resolve()
    encoded = os.environ.get("MACOS_DEVELOPER_ID_P12", "")
    certificate_password = os.environ.get("MACOS_DEVELOPER_ID_P12_PASSWORD", "")
    if not encoded or not certificate_password:
        raise RuntimeError("both MACOS_DEVELOPER_ID_P12 and MACOS_DEVELOPER_ID_P12_PASSWORD are required; no unsigned release")
    if platform.system() != "Darwin" or platform.machine() != "arm64":
        raise RuntimeError("release signing requires macOS Apple silicon")
    certificate = base64.b64decode(encoded, validate=True)
    env = {key: value for key, value in os.environ.items()
           if key not in ("MACOS_DEVELOPER_ID_P12", "MACOS_DEVELOPER_ID_P12_PASSWORD", "DEVELOPER_DIR")}

    def run(*argv):
        result = subprocess.run(argv, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if result.returncode:
            # Do not include argv: security's password arguments are secrets.
            raise RuntimeError(Path(argv[0]).name + " failed: " + result.stderr.strip())
        return result.stdout

    original_search_list = shlex.split(run("security", "list-keychains", "-d", "user"))
    repository = Path(__file__).resolve().parent.parent
    sources = repository / "apps/mac-dialog-decisions"
    with tempfile.TemporaryDirectory(prefix="ikesu-mac-dialog-release-") as work:
        work = Path(work)
        keychain = work / "signing.keychain-db"
        p12 = work / "developer.p12"
        with p12.open("wb") as stream:
            os.chmod(p12, 0o600)
            stream.write(certificate)
        keychain_password = secrets.token_urlsafe()
        created = False
        try:
            run("security", "create-keychain", "-p", keychain_password, str(keychain))
            created = True
            run("security", "unlock-keychain", "-p", keychain_password, str(keychain))
            run("security", "list-keychains", "-d", "user", "-s", str(keychain), *original_search_list)
            run("security", "import", str(p12), "-k", str(keychain), "-P", certificate_password,
                "-T", "/usr/bin/codesign", "-T", "/usr/bin/security")
            run("security", "set-key-partition-list", "-S", "apple-tool:,apple:,codesign:", "-s", "-k", keychain_password, str(keychain))
            package = work / "mac-dialog-decisions"
            executable = package / "IkesuDecisionDialog.app/Contents/MacOS/ikesu-decision-dialog"
            executable.parent.mkdir(parents=True)
            app = executable.parents[2]
            with (app / "Contents/Info.plist").open("wb") as stream:
                plistlib.dump({
                    "CFBundleIdentifier": "com.kazuph.ikesu.decisions",
                    "CFBundleName": "Ikesu Decisions", "CFBundleExecutable": executable.name,
                    "CFBundlePackageType": "APPL", "CFBundleVersion": version,
                    "CFBundleShortVersionString": version, "LSUIElement": True,
                }, stream)
            run("swiftc", "-O", *(str(sources / name) for name in
                ("dialog.swift", "main.swift", "host.swift", "preview.swift", "unicode-width.swift")), "-o", str(executable))
            run("codesign", "--force", "--options", "runtime", "--timestamp", "--sign", IDENTITY,
                "--keychain", str(keychain), str(app))
            run("codesign", "--verify", "--deep", "--strict", "-R", REQUIREMENT, str(app))
            stage_release_files(repository, package)
            (package / "VERSION").write_text(version + "\n")
            (package / "CHANNEL").write_text("release\n")
            output.mkdir(parents=True, exist_ok=True)
            archive = output / "ikesu-mac-dialog-darwin-arm64.tar.gz"
            if archive.exists() or (output / "SHA256SUMS").exists():
                raise RuntimeError("refusing to overwrite existing release artifacts")
            with tarfile.open(archive, "w:gz") as tar:
                tar.add(package, arcname="mac-dialog-decisions")
            with archive.open("rb") as stream:
                digest = hashlib.file_digest(stream, "sha256").hexdigest()
            (output / "SHA256SUMS").write_text(digest + "  " + archive.name + "\n")
        finally:
            try:
                run("security", "list-keychains", "-d", "user", "-s", *original_search_list)
            finally:
                if created:
                    run("security", "delete-keychain", str(keychain))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
