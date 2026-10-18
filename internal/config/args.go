package config

type BaseArgs struct {
	Verbose     bool   `short:"v" long:"verbose" description:"Show verbose information"`
	Debug       bool   `short:"d" long:"debug" description:"Show debug information"`
	LogFormat   string `long:"log-format" choice:"text" choice:"json" default:"text" description:"Format of the log on stderr"`
	ShowVersion bool   `long:"version" description:"Show version information and exit"`
}

type CLIArgs struct {
	BaseArgs
	CPUProfile  string `long:"cpu-profile" value-name:"FILE" description:"Write a CPU profile to FILE (for developers)"`
	MemProfile  string `long:"mem-profile" value-name:"FILE" description:"Write a memory profile to FILE when runtree exits (for developers)"`
	NetProfile  uint16 `long:"net-profile-port" value-name:"PORT" description:"Serve live profiles on http://localhost:PORT/debug/pprof/ (for developers)"`
	MaxDepth    uint   `long:"max-depth" description:"Maximum tree depth"`
	Check       bool   `long:"check" description:"Only check the tree file (and the included files), then exit"`
	Schema      bool   `long:"schema" description:"Print the JSON Schema of tree files (for editors), then exit"`
	RtrunnerDir string `long:"rtrunner-dir" value-name:"DIR" env:"RUNTREE_RTRUNNER_DIR" description:"A directory with rtrunner-<goos>-<goarch> binaries, to upload them to the hosts of ssh runners instead of the embedded ones"`
	NoSSHShare  bool   `long:"no-ssh-share" description:"Do not share one ssh connection for each host (ssh:// runners)"`
	Runner      string `long:"runner" value-name:"ADDRESS" description:"The runner of the nodes that do not specify one: local, host:port, tcp://host:port or ssh://[user@]host[:port][/path/to/rtrunner] (default: local)"`

	Lock       string `long:"lock" value-name:"FILE" description:"Lock FILE while the tree runs, another runtree with the same lock file exits (with 75) instead of running"`
	LogDB      string `long:"log-db" value-name:"FILE" description:"Save the events and the terminal output of the run in an SQLite database (it keeps all runs)"`
	ShowRuns   bool   `long:"show-runs" description:"List the runs of the --log-db database, then exit"`
	ShowEvents bool   `long:"show-events" description:"Print the events of a run from the --log-db database, then exit"`
	ShowOutput string `long:"show-output" value-name:"NODE" description:"Print the terminal output of a node (id, title or path) from the --log-db database, then exit"`
	Run        int64  `long:"run" value-name:"ID" description:"The run for --show-events and --show-output (default: the last one)"`
	Start      int    `long:"start" value-name:"N" description:"The start of the node for --show-output, 2 for the first re-run... (default: the last one)"`
}

const DefaultListenAddress = ":5000"

const DefaultMaxDepth = 30

type RtRunnerCLIArgs struct {
	BaseArgs
	ListenAddress string `short:"l" long:"listen" value-name:"ADDRESS" description:"Listen on ADDRESS (host:port, without a host on all interfaces; the connections are not authenticated)"`
	Stdio         bool   `long:"stdio" description:"Serve one runtree connection on stdin and stdout, then exit (for runner: ssh://...)"`
	CheckSHA256   string `long:"check-sha256" value-name:"HEX" description:"Check that the SHA-256 of this executable is HEX (after it is uploaded), then exit"`
}
