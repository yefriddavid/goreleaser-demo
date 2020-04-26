package main

import (
  "path"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/yefriddavid/remote-tail/cmd/command"
	"github.com/yefriddavid/remote-tail/cmd/console"

	"github.com/kevinburke/ssh_config"
	"github.com/spf13/viper"
	"strconv"
)

var mossSep = ".--. --- .-- . .-. . -..   -... -.--   -- -.-- .-.. -..- ... .-- \n"

var welcomeMessage = getWelcomeMessage() + console.ColorfulText(console.TextMagenta, mossSep)

var filePath = flag.String("file", "", "-file=\"/var/log/*.log\"")
var hostStr = flag.String("hosts", "", "-hosts=root@192.168.1.101,root@192.168.1.102")
var configFile = flag.String("conf", "", "-conf=example.yml")
var clusterName = flag.String("cluster",  "-cluster=example", "Cluster name for tail")
var tailFlags = flag.String("tail-flags", "--retry --follow=name", "flags for tail command, you can use -f instead if your server does't support `--retry --follow=name` flags")
var slient = flag.String("slient", "true", "-slient=false")
//**var slient = flag.Bool("slient", false, "-slient=false")

var Version = ""
var GitCommit = ""

func usageAndExit(message string) {

	if message != "" {
		fmt.Fprintln(os.Stderr, message)
	}

	flag.Usage()
	fmt.Fprint(os.Stderr, "\n")

	os.Exit(1)
}

type Servers struct {
	Name     string
	Enable   bool
	Alias    string
	TailFile string `mapstructure:"tail-file"`
}
type Settings struct {
	ConfigFile string `mapstructure:"config-file"`
	TailFile   string `mapstructure:"tail-file"`
  //Slient     bool
	Slient     string
}

func printWelcomeMessage(config command.Config) {
	fmt.Println(getWelcomeMessage())

	for _, server := range config.Servers {
		// If there is no tail_file for a service configuration, the global configuration is used
		if server.TailFile == "" {
			server.TailFile = config.TailFile
		}

		serverInfo := fmt.Sprintf("%s@%s:%s", server.User, server.Hostname, server.TailFile)
		fmt.Println(console.ColorfulText(console.TextMagenta, serverInfo))
	}
	fmt.Printf("\n%s\n", console.ColorfulText(console.TextCyan, mossSep))

}

func parseConfig(sourceConfiguration *viper.Viper, clusterName string, argSlient string, tailFlags string) (config command.Config) {

	if false { // is valid configuration
		log.Fatal("configuration source is not vali")
	} else {

		var settings Settings
		var servers []Servers
		sourceConfiguration.Unmarshal(&settings)
		sourceConfiguration.UnmarshalKey("clusters."+clusterName, &servers)
		config = command.Config{}
		config.TailFile = settings.TailFile

    var slient bool
    if argSlient == "" {
      slient = config.Slient
    } else {
      slient = argSlient == "true"
    }
    config.Slient = slient
		config.TailFlags = tailFlags
		f, _ := os.Open(settings.ConfigFile)
		cfg, _ := ssh_config.Decode(f)

		config.Servers = make(map[string]command.Server, len(servers))
		for key, server := range servers {
			if server.Enable == true {
				port, _ := cfg.Get(server.Name, "Port")
				hostname, _ := cfg.Get(server.Name, "HostName")
				user, _ := cfg.Get(server.Name, "User")
				privateKeyPath, _ := cfg.Get(server.Name, "IdentityFile")

				var serverPort int = 0
				if port == "" {
					serverPort, _ = strconv.Atoi(port)
				}
				newKey := strconv.Itoa(key)
				fmt.Println(server.TailFile)
				config.Servers["server_"+newKey] = command.Server{
					ServerName:     server.Alias, //"server_" + strconv.Itoa(key),
					PrivateKeyPath: privateKeyPath,
					Hostname:       strings.Trim(hostname, " "),
					User:           strings.Trim(user, " "),
					TailFile:       server.TailFile,
					Port:           serverPort,
				}
			}

		}

	}

	if config.TailFlags == "" {
		config.TailFlags = "--retry --follow=name"
	}

	return
}

func main() {

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, welcomeMessage)
		fmt.Fprint(os.Stderr, "Options:\n\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if (*filePath == "" || *hostStr == "") && *configFile == "" {
		usageAndExit("")
	}

	sourceConfiguration := getSourceConfigFile()
	config := parseConfig(sourceConfiguration, *clusterName, *slient, *tailFlags)
	if !config.Slient {
		printWelcomeMessage(config)
	}

	outputs := make(chan command.Message, 255)
	var wg sync.WaitGroup

	for _, server := range config.Servers {
		wg.Add(1)
		go func(server command.Server) {
			defer func() {
				if err := recover(); err != nil {
					fmt.Printf(console.ColorfulText(console.TextRed, "Error: %s\n"), err)
				}
			}()
			defer wg.Done()

			// If there is no tail_file for a service configuration, the global configuration is used
			if server.TailFile == "" {
				server.TailFile = config.TailFile
			}

			if server.TailFlags == "" {
				server.TailFlags = config.TailFlags
			}

			// If the service configuration does not have a port, the default value of 22 is used
			if server.Port == 0 {
				server.Port = 22
			}

			cmd := command.NewCommand(server)
			cmd.Execute(outputs)
		}(server)
	}

	if len(config.Servers) > 0 {
		go func() {
			for output := range outputs {
				content := strings.Trim(output.Content, "\r\n")

				if content == "" || (strings.HasPrefix(content, "==>") && strings.HasSuffix(content, "<==")) {
					continue
				}

				if config.Slient {
					fmt.Printf("%s -> %s\n", output.Host, content)
				} else {
					fmt.Printf(
						"%s %s %s\n",
						console.ColorfulText(console.TextGreen, output.Host),
						console.ColorfulText(console.TextYellow, "->"),
						content,
					)
				}
			}
		}()
	} else {
		fmt.Println(console.ColorfulText(console.TextRed, "No target host is available"))
	}

	wg.Wait()
}

func getSourceConfigFile() *viper.Viper {
	v := viper.New()
  if *configFile == "" {
    v.AddConfigPath("./")
    //**v.SetConfigName("defaultFileName")
  } else {
    dir, file := path.Split(*configFile)
    ext := path.Ext(file)
    var absoluteFileName string
    if ext == "" {
      absoluteFileName = file
    } else {
      absoluteFileName = strings.TrimRight(file, ext)
    }

    v.AddConfigPath(dir)
    v.SetConfigName(absoluteFileName)

  }
	var err error
	err = v.ReadInConfig()
	if err != nil {
		fmt.Println(err)
	}
	return v
}

func getWelcomeMessage() string {
	return `
 ____                      _      _____     _ _
|  _ \ ___ _ __ ___   ___ | |_ __|_   _|_ _(_) |
| |_) / _ \ '_ ' _ \ / _ \| __/ _ \| |/ _' | | |
|  _ <  __/ | | | | | (_) | ||  __/| | (_| | | |
|_| \_\___|_| |_| |_|\___/ \__\___||_|\__,_|_|_|

Author: mylxsw
Homepage: github.com/mylxsw/remote-tail
Version: ` + Version + "(" + GitCommit + ")" + `
`
}



