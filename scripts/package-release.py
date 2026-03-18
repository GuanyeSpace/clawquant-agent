from __future__ import annotations

import argparse
import io
import tarfile
from pathlib import Path


EXCLUDED_NAMES = {
    "__pycache__",
    ".pytest_cache",
}

EXCLUDED_SUFFIXES = {
    ".pyc",
    ".pyo",
}


def main() -> int:
    args = parse_args()
    dist_dir = args.dist_dir.resolve()
    sdk_dir = args.sdk_dir.resolve()
    readme = args.readme.resolve()
    install_script = args.install_script.resolve()
    service_file = args.service_file.resolve()

    targets = [item.strip() for item in args.targets.split(",") if item.strip()]
    if not targets:
        raise SystemExit("at least one target is required")

    for target in targets:
        goos, goarch = parse_target(target)
        binary_dir = dist_dir / f"{args.project_name}-{goos}-{goarch}"
        binary_path = binary_dir / args.project_name
        if not binary_path.exists():
            raise SystemExit(f"missing build artifact: {binary_path}")

        package_path = dist_dir / f"{args.project_name}-{goos}-{goarch}.tar.gz"
        build_package(
            package_path=package_path,
            binary_path=binary_path,
            sdk_dir=sdk_dir,
            readme=readme,
            install_script=install_script,
            service_file=service_file,
        )
        print(f"Created {package_path}")

    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Create Linux release archives for clawquant-agent.")
    parser.add_argument("--project-name", default="clawquant-agent")
    parser.add_argument("--dist-dir", type=Path, default=Path("dist"))
    parser.add_argument("--sdk-dir", type=Path, default=Path("sdk"))
    parser.add_argument("--readme", type=Path, default=Path("README.md"))
    parser.add_argument("--install-script", type=Path, default=Path("scripts/install.sh"))
    parser.add_argument("--service-file", type=Path, default=Path("scripts/clawquant-agent.service"))
    parser.add_argument("--targets", default="linux/amd64,linux/arm64")
    return parser.parse_args()


def parse_target(target: str) -> tuple[str, str]:
    parts = target.split("/", 1)
    if len(parts) != 2 or not parts[0] or not parts[1]:
        raise SystemExit(f"invalid target: {target}")
    return parts[0], parts[1]


def build_package(
    *,
    package_path: Path,
    binary_path: Path,
    sdk_dir: Path,
    readme: Path,
    install_script: Path,
    service_file: Path,
) -> None:
    package_path.parent.mkdir(parents=True, exist_ok=True)
    with tarfile.open(package_path, "w:gz") as tar:
        add_file(tar, binary_path, "clawquant-agent", mode=0o755)
        add_text_file(tar, install_script, "install.sh", mode=0o755)
        add_text_file(tar, service_file, "clawquant-agent.service", mode=0o644)
        add_file(tar, readme, "README.md", mode=0o644)
        add_sdk_tree(tar, sdk_dir)


def add_sdk_tree(tar: tarfile.TarFile, sdk_dir: Path) -> None:
    if not sdk_dir.is_dir():
        raise SystemExit(f"sdk directory not found: {sdk_dir}")

    for path in sorted(sdk_dir.rglob("*")):
        relative = path.relative_to(sdk_dir)
        if should_skip(relative):
            continue
        arcname = Path("sdk") / relative
        if path.is_dir():
            add_directory(tar, arcname.as_posix())
            continue
        add_file(tar, path, arcname.as_posix(), mode=file_mode(path))


def should_skip(relative: Path) -> bool:
    parts = set(relative.parts)
    if parts & EXCLUDED_NAMES:
        return True
    if relative.suffix in EXCLUDED_SUFFIXES:
        return True
    if any(part.endswith(".egg-info") for part in relative.parts):
        return True
    if relative.parts and relative.parts[0] == "tests":
        return True
    return False


def add_directory(tar: tarfile.TarFile, arcname: str) -> None:
    info = tarfile.TarInfo(arcname.rstrip("/") + "/")
    info.type = tarfile.DIRTYPE
    info.mode = 0o755
    tar.addfile(info)


def add_file(tar: tarfile.TarFile, src: Path, arcname: str, *, mode: int) -> None:
    data = src.read_bytes()
    info = tarfile.TarInfo(arcname)
    info.size = len(data)
    info.mode = mode
    tar.addfile(info, io.BytesIO(data))


def add_text_file(tar: tarfile.TarFile, src: Path, arcname: str, *, mode: int) -> None:
    data = src.read_text(encoding="utf-8").replace("\r\n", "\n").replace("\r", "\n").encode("utf-8")
    info = tarfile.TarInfo(arcname)
    info.size = len(data)
    info.mode = mode
    tar.addfile(info, io.BytesIO(data))


def file_mode(path: Path) -> int:
    if path.suffix == ".sh":
        return 0o755
    return 0o644


if __name__ == "__main__":
    raise SystemExit(main())
