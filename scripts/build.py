import os
import sys
import re
import platform
import copy
import datetime
import gzip
import shutil
import subprocess as sp

targets=["rtrunner", "runtree", "runtree-lsp"]

DIR=os.path.abspath(os.path.join(__file__,os.pardir,os.pardir))

module_line = next(open(os.path.join(DIR, "go.mod"), "r"))
module_name = re.match(r"module\s+(.+)", module_line).group(1)
version_prefix=f"{module_name}/internal/version"


oss = platform.system().lower()
if oss not in ["windows", "linux"]:
	raise SystemExit(f"unknown system {oss}")

is_mingw = False
try:
	import sysconfig
	is_mingw = "mingw" in sysconfig.get_platform().lower()
except ImportError:
	pass

arch = platform.machine().lower()
if arch=='x86_64':
	arch = "amd64"
if arch not in ["amd64", "i386"]:
	raise SystemExit(f"unknown arch {arch}")

print(f"os={oss} arch={arch}")

def co(cmd):
	o = sp.check_output(cmd)
	return o.decode("utf-8").strip()

def run(cmd):
	print(cmd[0])
	for item in cmd[1:]:
		print("",item)
	ret = sp.call(cmd)
	if ret != 0:
		raise SystemExit(ret)

built=datetime.datetime.now().isoformat()
branch = co(["git", "rev-parse", "--abbrev-ref", "HEAD"])
commit = co(["git", "rev-parse", "HEAD"])

print(f"built={built} branch={branch} commit={commit}")

ext = ""
ldflags=[
	"-X", f"{version_prefix}.Built={built}",
	"-X", f"{version_prefix}.Commit={commit}",
	"-X", f"{version_prefix}.Branch={branch}",	
]
ldflags_debug = copy.copy(ldflags)
ldflags += ["-s", "-w"]
ldflags_gui = copy.copy(ldflags)
if oss=="windows" and not is_mingw:
	ldflags += ["-tags", "timetzdata"]
	ldflags_gui += ["-H=windowsgui"]
	ext = ".exe"

os.environ["GOOS"] = oss
os.environ["GOARCH"] = arch
ddir = os.path.join(DIR, "dist", oss, arch)
if not os.path.isdir(ddir):
	os.makedirs(ddir)

os.chdir(DIR)
print("go mod tidy")
run(["go", "mod", "tidy"])

# The rtrunner binaries that runtree uploads to the hosts of managed ssh runners (ssh://host without a path): they are
# embedded in runtree (-tags rtrunners), and they are in dist/rtrunners too (for manual installs, or --rtrunner-dir).
rtrunner_platforms = [
	("linux", "amd64"), ("linux", "arm64"), ("linux", "arm"),
	("darwin", "amd64"), ("darwin", "arm64"),
]
embed_dir = os.path.join(DIR, "cmd", "runtree", "rtrunners")
rtrunners_dist = os.path.join(DIR, "dist", "rtrunners")
for d in (embed_dir, rtrunners_dist):
	shutil.rmtree(d, ignore_errors=True)
	os.makedirs(d)
for goos, goarch in rtrunner_platforms:
	name = f"rtrunner-{goos}-{goarch}"
	out = os.path.join(rtrunners_dist, name)
	cmd = ["go", "build", "-ldflags", " ".join(ldflags_debug + ["-s", "-w"]), "-o", out, "./cmd/rtrunner"]
	print(f"GOOS={goos} GOARCH={goarch} " + " ".join(cmd))
	# GOARM=6 runs on every 32 bit ARM Linux (Raspberry Pi 1 too)
	ret = sp.call(cmd, env=dict(os.environ, GOOS=goos, GOARCH=goarch, GOARM="6", CGO_ENABLED="0"))
	if ret != 0:
		raise SystemExit(ret)
	with open(out, "rb") as f, open(os.path.join(embed_dir, name + ".gz"), "wb") as raw, \
			gzip.GzipFile(filename="", mode="wb", compresslevel=9, fileobj=raw, mtime=0) as g:
		shutil.copyfileobj(f, g)

for target in targets:
	targetdir = os.path.join(DIR, "cmd", target)
	if not os.path.isdir(targetdir):
		print(f"package directory {targetdir} not found")
		raise SystemExit(1)
	os.chdir(targetdir)
	src = "." # the package, all of its files
	# only the GUI needs cgo; the others are static, so they run on other distributions too (e.g. rtrunner on a host)
	os.environ["CGO_ENABLED"] = "1" if target == "runtree" else "0"
	binary = os.path.join(ddir, target + ext)
	winres = os.path.join(targetdir, "winres", "winres.json")
	is_gui = os.path.isfile(winres)
	if is_gui:
		sldflags = " ".join(ldflags_gui)
	else:
		sldflags = " ".join(ldflags)
	# runtree embeds the rtrunner binaries
	tags = ["-tags", "rtrunners"] if target == "runtree" else []
	cmd = ["go", "build"] + tags + ["-ldflags", sldflags, "-o", binary, src]
	print(" ".join(cmd))
	run(cmd)
	if is_gui and oss=="windows":
		run(["go-winres", "patch", "--no-backup", binary])


	binary = os.path.join(ddir, target + "_debug" + ext)
	sldflags = " ".join(ldflags_debug)
	cmd = ["go", "build"] + tags + ["-ldflags", sldflags, "-o", binary, src]
	print(" ".join(cmd))
	run(cmd)
	if is_gui and oss=="windows":
		run(["go-winres", "patch", "--no-backup", binary])


