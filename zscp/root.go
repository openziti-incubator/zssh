package zscp

import (
	"fmt"
	"github.com/openziti/sdk-golang/ziti"
	"github.com/openziti/sdk-golang/ziti/config"
	"github.com/pkg/sftp"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"io/fs"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"zssh/zsshlib"
)

const ExpectedServiceAndExeName = "zssh"

var (
	ZConfig    string
	SshKeyPath string
	debug      bool
	recursive  bool

	rootCmd = &cobra.Command{
		Use: "Remote to Local: zscp <remoteUsername>@<targetIdentity>:[Remote Path] [Local Path]\n" +
			"Local to Remote: zscp [Local Path] <remoteUsername>@<targetIdentity>:[Remote Path]",
		Short: "Z(iti)scp, Carb-loaded ssh performs faster and stronger than ssh",
		Long:  "Z(iti)scp is a version of ssh that utilizes a ziti network to provide a faster and more secure remote connection. A ziti connection must be established before use",
		Args:  cobra.ExactValidArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if SshKeyPath == "" {
				userHome, err := os.UserHomeDir()
				if err != nil {
					logrus.Fatal(err)
				}
				SshKeyPath = filepath.Join(userHome, ".ssh", "id_rsa")
			}
			if debug {
				logrus.Infof("    sshKeyPath set to: %s", SshKeyPath)
			}

			if ZConfig == "" {
				userHome, err := os.UserHomeDir()
				if err != nil {
					logrus.Fatal(err)
				}
				ZConfig = filepath.Join(userHome, ".ziti", fmt.Sprintf("%s.json", ExpectedServiceAndExeName))
			}
			if debug {
				logrus.Infof("       ZConfig set to: %s", ZConfig)
			}

			var username string
			var targetIdentity string
			var remoteFilePath string
			var localFilePath string
			var isCopyToRemote bool

			if strings.ContainsAny(args[0], ":") {
				remoteFilePath = args[0]
				localFilePath = args[1]
				isCopyToRemote = false

			} else if strings.ContainsAny(args[1], ":") {
				remoteFilePath = args[1]
				localFilePath = args[0]
				isCopyToRemote = true
			} else {
				logrus.Fatal("cannot determine remote file PATH use \":\" for remote path")
			}

			fullRemoteFilePath := strings.Split(remoteFilePath, ":")
			remoteFilePath = fullRemoteFilePath[1]

			if strings.ContainsAny(fullRemoteFilePath[0], "@") {
				userServiceName := strings.Split(fullRemoteFilePath[0], "@")
				username = userServiceName[0]
				targetIdentity = userServiceName[1]

			} else {
				curUser, err := user.Current()
				if err != nil {
					logrus.Fatal(err)
				}
				username = curUser.Username
				if strings.Contains(username, "\\") && runtime.GOOS == "windows" {
					username = strings.Split(username, "\\")[1]
				}
				targetIdentity = args[0]
			}
			if debug {
				logrus.Infof("      username set to: %s", username)
				logrus.Infof("targetIdentity set to: %s", targetIdentity)
			}

			ctx := ziti.NewContextWithConfig(getConfig(ZConfig))

			_, ok := ctx.GetService(ExpectedServiceAndExeName)
			if !ok {
				logrus.Fatal("error when retrieving all the services for the provided config")
			}

			dialOptions := &ziti.DialOptions{
				ConnectTimeout: 0,
				Identity:       targetIdentity,
				AppData:        nil,
			}
			svc, err := ctx.DialWithOptions(ExpectedServiceAndExeName, dialOptions)
			defer func() { _ = svc.Close() }()
			if err != nil {
				logrus.Fatal(fmt.Sprintf("error when dialing service name %s. %v", ExpectedServiceAndExeName, err))
			}
			factory := zsshlib.NewSshConfigFactoryImpl(username, SshKeyPath)
			config := factory.Config()
			sshConn, err := zsshlib.Dial(config, svc)
			if err != nil{
				logrus.Fatal(err, "error dialing SSH Conn")
			}
			client, err := sftp.NewClient(sshConn)
			if err != nil {
				logrus.Fatal(err, "error creating sftp client")
			}
			defer func() { _ = client.Close() }()

			if isCopyToRemote {
				if recursive {
					err := filepath.WalkDir(localFilePath, func(path string, info fs.DirEntry, err error) error {
						remoteDestination := filepath.Join(remoteFilePath,filepath.Base(path))
						if info.IsDir() {
							err = client.Mkdir(remoteDestination)
							if err != nil {
								logrus.Error(err)
							} else if debug {
								logrus.Infof("made directory: %s", remoteDestination)
							}
						} else {
							err = zsshlib.SendFile(client, path, remoteDestination)
							if err != nil {
								return err
							} else if debug{
								logrus.Infof("sent file: %s ==> %s", path, remoteDestination)
							}
						}
						return nil
					})
					if err != nil {
						logrus.Fatal(err)
					}
				} else {
					zsshlib.SendFile(client, localFilePath, remoteFilePath)
				}
			} else {
				zsshlib.RetrieveRemoteFiles(factory, svc, localFilePath, remoteFilePath)
			}
		},
	}
)

func init() {
	rootCmd.Flags().StringVarP(&ZConfig, "ZConfig", "c", "", fmt.Sprintf("Path to ziti config file. default: $HOME/.ziti/%s.json", ExpectedServiceAndExeName))
	rootCmd.Flags().StringVarP(&SshKeyPath, "SshKeyPath", "i", "", "Path to ssh key. default: $HOME/.ssh/id_rsa")
	rootCmd.Flags().BoolVarP(&debug, "debug", "d", false, "pass to enable additional debug information")
	rootCmd.Flags().BoolVarP(&recursive,"recursive", "r",false, "pass to enable recursive file transfer")
}

type ServiceConfig struct {
	Protocol string
	Hostname string
	Port     int
}

func Execute() error {
	return rootCmd.Execute()
}

func getConfig(cfgFile string) (zitiCfg *config.Config) {
	zitiCfg, err := config.NewFromFile(cfgFile)
	if err != nil {
		log.Fatalf("failed to load ziti configuration file: %v", err)
	}
	return zitiCfg
}
