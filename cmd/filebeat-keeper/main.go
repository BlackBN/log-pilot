package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"syscall"
	"text/template"

	"gopkg.in/yaml.v2"

	"github.com/caicloud/logging-admin/pkg/util/graceful"
	"github.com/caicloud/logging-admin/pkg/util/osutil"

	"github.com/caicloud/nirvana/log"
	"gopkg.in/fsnotify/fsnotify.v1"
)

var (
	filebeatExecutablePath = osutil.Getenv("FB_EXE_PATH", "filebeat")
	srcConfigPath          = osutil.Getenv("SRC_CONFIG_PATH", "/config/filebeat-output.yml")
	dstConfigPath          = osutil.Getenv("DST_CONFIG_PATH", "/etc/filebeat/")
)

// When configmap being created for the first time, following events received:
// INFO  1206-09:38:39.496+00 main.go:41 | Event: "/config/..2018_12_06_09_38_39.944532540": CREATE
// INFO  1206-09:38:39.496+00 main.go:41 | Event: "/config/..2018_12_06_09_38_39.944532540": CHMOD
// INFO  1206-09:38:39.497+00 main.go:41 | Event: "/config/filebeat-output.yml": CREATE
// INFO  1206-09:38:39.497+00 main.go:41 | Event: "/config/..data_tmp": RENAME
// INFO  1206-09:38:39.497+00 main.go:41 | Event: "/config/..data": CREATE
// INFO  1206-09:38:39.497+00 main.go:41 | Event: "/config/..2018_12_06_09_37_32.878326343": REMOVE
// When configmap being modified, following events received:
// INFO  1206-09:42:56.488+00 main.go:41 | Event: "/config/..2018_12_06_09_42_56.160544363": CREATE
// INFO  1206-09:42:56.488+00 main.go:41 | Event: "/config/..2018_12_06_09_42_56.160544363": CHMOD
// INFO  1206-09:42:56.488+00 main.go:41 | Event: "/config/..data_tmp": RENAME
// INFO  1206-09:42:56.488+00 main.go:41 | Event: "/config/..data": CREATE
// INFO  1206-09:42:56.488+00 main.go:41 | Event: "/config/..2018_12_06_09_38_39.944532540": REMOVE
func watchFileChange(path string, reloadCh chan<- struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(path); err != nil {
		return err
	}

	for {
		select {
		case ev := <-w.Events:
			log.Infoln("Event:", ev.String())
			if ev.Op&fsnotify.Create == fsnotify.Create {
				if filepath.Base(ev.Name) == "..data" {
					log.Infoln("Configmap updated")
					reloadCh <- struct{}{}
				}
			}
		case err := <-w.Errors:
			log.Errorf("Watch error: %v", err)
		}
	}
}

func run(stopCh <-chan struct{}) error {
	reloadCh := make(chan struct{}, 1)
	started := false

	go watchFileChange(filepath.Dir(srcConfigPath), reloadCh)

	var cmds []*exec.Cmd
	if newCmds, err := applyChange(); err == nil {
		cmds = newCmds
		reloadCh <- struct{}{}
	} else {
		log.Errorf("Error generate config file: %v", err)
		log.Infoln("Filebeat will not start until configmap being updated")
	}

	for {
		select {
		case <-stopCh:
			log.Infoln("Wait filebeat shutdown")
			for _, cmd := range cmds {
				if err := cmd.Wait(); err != nil {
					return fmt.Errorf("filebeat quit with error: %v", err)
				}
			}
			return nil
		case <-reloadCh:
			log.Infoln("Reload")
			newCmds, err := applyChange()
			if err != nil {
				log.Errorln("Error apply change:", err)
				continue
			}
			if started {
				for _, cmd := range cmds {
					log.Infoln("Send TERM signal")
					if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
						return fmt.Errorf("error send signal: %v", err)
					}
					if err := cmd.Wait(); err != nil {
						return fmt.Errorf("filebeat quit with error: %v", err)
					}
					log.Infoln("Filebeat quit")
				}
			}
			for _, cmd := range newCmds {
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("error run filebeat: %v", err)
				}
			}
			started = true
			cmds = newCmds
		}
	}
}

func applyChange() ([]*exec.Cmd, error) {
	outputCfgData, err := ioutil.ReadFile(srcConfigPath)
	if err != nil {
		return nil, err
	}

	tmplData, err := ioutil.ReadFile("/etc/filebeat/filebeat.yml.tpl")
	if err != nil {
		return nil, err
	}

	cfg := make([]map[string]interface{}, 0)
	if err := yaml.Unmarshal(outputCfgData, &cfg); err != nil {
		return nil, fmt.Errorf("error decode output config yaml: %v", err)
	}

	t, err := template.New("filebeat").Parse(string(tmplData))
	if err != nil {
		return nil, err
	}

	var cmds []*exec.Cmd
	for i, config := range cfg {
		fName := path.Join(dstConfigPath, fmt.Sprintf("filebeat_%d.yml", i))
		f, err := os.OpenFile(fName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return nil, err
		}
		if err := t.Execute(f, config); err != nil {
			f.Close()
			return nil, fmt.Errorf("error rendor template: %v", err)
		}
		f.Close()

		generated, _ := ioutil.ReadFile(fName)
		fmt.Println(string(generated))
		// create cmd
		cmds = append(cmds, newCmd("--c="+fName))
	}
	return cmds, nil
}

var (
	fbArgs []string
)

func newCmd(configPathArg string) *exec.Cmd {
	args := append(fbArgs, configPathArg)
	log.Infof("Will run filebeat with command: %v %v", filebeatExecutablePath, args)
	cmd := exec.Command(filebeatExecutablePath, args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	return cmd
}

func main() {
	fbArgs = os.Args[1:]
	os.Args = os.Args[:1]
	flag.Parse()

	closeCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		if err := run(closeCh); err != nil {
			log.Fatalln("Error run keeper:", err)
		}
		wg.Done()
	}()
	go graceful.HandleSignal(closeCh, nil)
	wg.Wait()
}
