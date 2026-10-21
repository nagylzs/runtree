"""Packages the binaries that scripts/build.py built into release archives, in dist/release/.

    python3 scripts/package.py linux-amd64 windows-amd64 rtrunners    # on Linux (CI)
    python3 scripts/package.py darwin-arm64                          # on macOS

A platform (os-arch) becomes runtree-<version>-<os>-<arch>.tar.gz (a .zip for Windows) with the programs of
dist/<os>/<arch>/ (not the _debug ones), README.md, LICENSE and the examples. "rtrunners" copies the binaries of
dist/rtrunners/ (the rtrunner of each platform, for manual installs) as they are. The version is the one of build.py.
"""
import os
import shutil
import sys
import tarfile
import zipfile

from gitversion import git_version

DIR = os.path.abspath(os.path.join(__file__, os.pardir, os.pardir))
programs = ["runtree", "runtree-headless", "rtrunner", "runtree-lsp"]
extras = ["README.md", "LICENSE"]


def files_of(goos, goarch):
	"""The files of a platform archive: (path on disk, path in the archive)"""
	ext = ".exe" if goos == "windows" else ""
	src = os.path.join(DIR, "dist", goos, goarch)
	files = []
	for p in programs:
		path = os.path.join(src, p + ext)
		if os.path.isfile(path):
			files.append((path, p + ext))
	if not files:
		raise SystemExit(f"no programs in {src}: run scripts/build.py first")
	for e in extras:
		files.append((os.path.join(DIR, e), e))
	for name in sorted(os.listdir(os.path.join(DIR, "examples"))):
		path = os.path.join(DIR, "examples", name)
		if os.path.isfile(path):
			files.append((path, "examples/" + name))
	for name in sorted(os.listdir(os.path.join(DIR, "examples", "lib"))):
		files.append((os.path.join(DIR, "examples", "lib", name), "examples/lib/" + name))
	return files


def package(platform, ver, out):
	if platform == "rtrunners":
		src = os.path.join(DIR, "dist", "rtrunners")
		for name in sorted(os.listdir(src)):
			dst = os.path.join(out, name)
			shutil.copy2(os.path.join(src, name), dst)
			print(dst)
		return
	goos, goarch = platform.split("-")
	base = f"runtree-{ver}-{goos}-{goarch}"
	files = files_of(goos, goarch)
	if goos == "windows":
		path = os.path.join(out, base + ".zip")
		with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as z:
			for src, name in files:
				z.write(src, base + "/" + name)
	else:
		path = os.path.join(out, base + ".tar.gz")
		with tarfile.open(path, "w:gz") as t:
			for src, name in files:
				info = t.gettarinfo(src, base + "/" + name)
				info.uid = info.gid = 0
				info.uname = info.gname = ""
				with open(src, "rb") as f:
					t.addfile(info, f)
	print(path)


def main():
	if len(sys.argv) < 2:
		raise SystemExit(__doc__)
	ver = git_version(DIR)
	out = os.path.join(DIR, "dist", "release")
	os.makedirs(out, exist_ok=True)
	print(f"version={ver}")
	for platform in sys.argv[1:]:
		package(platform, ver, out)


if __name__ == "__main__":
	main()
