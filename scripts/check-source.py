#!/usr/bin/env python3
"""Check this repository's publishable source inventory, without reading secrets.

This is a narrow preflight, not a replacement for a dedicated secret scanner.
Ignored local build/tool directories are not read. Tracked ignored files are still
checked, so accidentally committed artifacts cannot hide behind .gitignore.
"""

from pathlib import Path
import re
import subprocess
import sys
import xml.etree.ElementTree as ET


ROOT = Path(__file__).resolve().parents[1]
GENERATED_DIRS = {".tools", "build", "bin", "obj", "reports", "__pycache__", "node_modules"}
GENERATED_SUFFIXES = {".exe", ".dll", ".pdb", ".o", ".obj", ".a", ".lib", ".so", ".dylib",
                      ".nupkg", ".snupkg", ".pyc", ".zip", ".tar", ".gz", ".dmp", ".core"}
SECRET_PATTERNS = {
    "private key": re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"),
    "GitHub token": re.compile(rb"\b(?:gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,})\b"),
    "AWS access key": re.compile(rb"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b"),
    "credential URL": re.compile(rb"https?://[^\s/:@]+:[^\s/@]+@[^\s]+"),
}


def main():
    inventory = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT
    ).decode("utf-8").split("\0")
    files = sorted(set(filter(None, inventory)))
    problems = []

    def fail(name, message):
        problems.append(f"{name}: {message}")

    for name in files:
        relative = Path(name)
        path = ROOT / relative
        if path.is_symlink():
            fail(name, "symlinks require a separate release review")
            continue
        if not path.is_file():
            fail(name, "missing or non-regular tracked source")
            continue
        if set(relative.parts) & GENERATED_DIRS or relative.suffix.lower() in GENERATED_SUFFIXES:
            fail(name, "generated artifact is in the publishable source inventory")
            continue
        if path.stat().st_size > 1024 * 1024:
            fail(name, "source file exceeds the 1 MiB review limit")
            continue
        data = path.read_bytes()
        if b"\0" in data:
            fail(name, "binary data is in the publishable source inventory")
            continue
        for kind, pattern in SECRET_PATTERNS.items():
            if pattern.search(data):
                fail(name, f"possible {kind}; matched value deliberately omitted")
        if relative.suffix == ".csproj":
            project = ET.fromstring(data)
            references = project.findall(".//PackageReference")
            for reference in references:
                version = reference.get("Version") or reference.findtext("Version") or ""
                if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.]+)?", version):
                    fail(name, "PackageReference must specify an exact version")
            lock_name = relative.with_name("packages.lock.json").as_posix()
            if references and lock_name not in files:
                fail(name, "PackageReference requires a committed packages.lock.json")
        if name.startswith(".github/workflows/"):
            for action in re.findall(rb"\buses:\s*([^\s#]+)", data):
                if action.startswith(b"./"):
                    continue
                if not re.fullmatch(rb"[A-Za-z0-9_.\-/]+@[0-9a-f]{40}", action):
                    fail(name, "remote GitHub Actions must use a full commit SHA")

    module = (ROOT / "go.mod").read_text()
    if re.search(r"(?m)^require\s", module) and "go.sum" not in files:
        fail("go.mod", "third-party Go modules require a committed go.sum")
    for problem in problems:
        print(problem, file=sys.stderr)
    print(f"Source preflight: {len(files)} files, {len(problems)} problem(s).")
    return bool(problems)


if __name__ == "__main__":
    sys.exit(main())
