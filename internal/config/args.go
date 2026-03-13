package config

type BaseArgs struct {
	Verbose     bool   `short:"v" long:"verbose" description:"Show verbose information"`
	Debug       bool   `short:"d" long:"debug" description:"Show debug information"`
	ShowVersion bool   `long:"version" description:"Show version information and exit"`
	CPUProfile  string `long:"cpu-profile" description:"Write CPU profile to file"`
	MemProfile  string `long:"mem-profile" description:"Write MEM profile to file"`
	NetProfile  uint16 `long:"net-profile-port" description:"Start profile http server on PORT"`
}

type CLIArgs struct {
	BaseArgs
	MaxDepth uint `long:"max-depth" description:"Maximum tree depth"`
}

const DefaultListenAddress = ":5000"
const DefaultRunnerAddress = "127.0.0.1:5000"

const DefaultMaxDepth = 30

type RtRunnerCLIArgs struct {
	BaseArgs
	ListenAddress string `short:"l" long:"listen" description:"Listen address, defaults to :5000"`
}
