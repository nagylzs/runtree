"""The version of a build, from git: used by build.py and package.py."""
import subprocess as sp


def git_version(cwd=None):
	"""The tag of the commit (v0.1.0), or the last tag and the commits after it (v0.1.0-5-g1be6ff0e93e1), with -dirty
	when there are uncommitted changes; v0.0.0-g<commit> before the first tag (or without the tags, e.g. a shallow
	clone)."""
	def out(cmd):
		return sp.check_output(cmd, cwd=cwd, stderr=sp.DEVNULL).decode("utf-8").strip()
	try:
		return out(["git", "describe", "--tags", "--match", "v[0-9]*", "--abbrev=12", "--dirty"])
	except sp.CalledProcessError:
		dirty = "-dirty" if out(["git", "status", "--porcelain", "--untracked-files=no"]) else ""
		return "v0.0.0-g" + out(["git", "rev-parse", "--short=12", "HEAD"]) + dirty
