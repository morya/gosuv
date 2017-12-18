package main

const (
	DefaultConfig string = "config.yml"
	DefaultProgramFile string = "programs.yml"
	DefaultPidFile string = ".gosuv.pid"
	DefaultSockFile string = ".gosuv.sock"
	DefaultGoSuvLogFile string ="gosuv.log"
	AppName string = "gosuv"
	Version string = "201712041615"
	Author string = "codeskyblue,modify by ajian521"
	Email string = "ajian521@gmail.com"
)

// Global var
var (
	Cfg     Configuration
    CfgDir string
    CfgFile string
    CmdDir string   //命令行所在目录
    CurrentDir string //当前命令执行所在目录
)

type Program struct {
	Name          string   `yaml:"name" json:"name"`
	Command       string   `yaml:"command" json:"command"`
	Environ       []string `yaml:"environ" json:"environ"`
	Dir           string   `yaml:"directory" json:"directory"`
	StartAuto     bool     `yaml:"start_auto" json:"startAuto"`
	StartRetries  int      `yaml:"start_retries" json:"startRetries"`
	StartSeconds  int      `yaml:"start_seconds,omitempty" json:"startSeconds"`
	StopTimeout   int      `yaml:"stop_timeout,omitempty" json:"stopTimeout"`
	User          string   `yaml:"user,omitempty" json:"user"`
	LogDisable    bool     `yaml:"log_disable" json:"log_disable"`
	StderrOnly    bool    `yaml:"stderr_only,omitempty" json:"stderr_only"`
	Notifications struct {
		Pushover struct {
			ApiKey string   `yaml:"api_key"`
			Users  []string `yaml:"users"`
		} `yaml:"pushover,omitempty"`
	} `yaml:"notifications,omitempty" json:"-"`
	WebHook struct {
		Github struct {
			Secret string `yaml:"secret"`
		} `yaml:"github"`
		Command string `yaml:"command"`
		Timeout int    `yaml:"timeout"`
	} `yaml:"webhook,omitempty" json:"-"`
}
