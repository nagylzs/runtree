package config

type BaseArgs struct {
	Verbose     bool   `short:"v" long:"verbose" description:"Show verbose information"`
	Debug       bool   `short:"d" long:"debug" description:"Show debug information"`
	LogFormat   string `long:"log-format" choice:"text" choice:"json" default:"text" description:"Format of the log on stderr"`
	ShowVersion bool   `long:"version" description:"Show version information and exit"`
	CPUProfile  string `long:"cpu-profile" description:"Write CPU profile to file"`
	MemProfile  string `long:"mem-profile" description:"Write MEM profile to file"`
	NetProfile  uint16 `long:"net-profile-port" description:"Start profile http server on PORT"`
}

type CLIArgs struct {
	BaseArgs
	MaxDepth    uint   `long:"max-depth" description:"Maximum tree depth"`
	Check       bool   `long:"check" description:"Only check the tree file (and the included files), then exit"`
	Schema      bool   `long:"schema" description:"Print the JSON Schema of tree files (for editors), then exit"`
	RtrunnerDir string `long:"rtrunner-dir" value-name:"DIR" env:"RUNTREE_RTRUNNER_DIR" description:"A directory with rtrunner-<goos>-<goarch> binaries, to upload them to the hosts of ssh runners instead of the embedded ones"`
	NoSSHShare  bool   `long:"no-ssh-share" description:"Do not share one ssh connection for each host (ssh:// runners)"`
	Runner      string `long:"runner" value-name:"ADDRESS" description:"The runner of the nodes that do not specify one: local, host:port, tcp://host:port or ssh://[user@]host[:port][/path/to/rtrunner] (default: local)"`

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
	ListenAddress string `short:"l" long:"listen" description:"Listen address, defaults to :5000"`
	Stdio         bool   `long:"stdio" description:"Serve one runtree connection on stdin and stdout, then exit (for runner: ssh://...)"`
	CheckSHA256   string `long:"check-sha256" value-name:"HEX" description:"Check that the SHA-256 of this executable is HEX (after it is uploaded), then exit"`
}
