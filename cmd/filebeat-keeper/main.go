package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/caicloud/logging-admin/pkg/util/graceful"
	"github.com/caicloud/logging-admin/pkg/util/osutil"

	"github.com/caicloud/nirvana/log"
)

const (
	HeatlthCheckInterval = "HEATLTH_CHECK_INTERVAL"
	ConfigCheckInterval  = "CONFIG_CHECK_INTERVAL"
)

var (
	filebeatExecutablePath = osutil.Getenv("FB_EXE_PATH", "filebeat")
	srcConfigPath          = osutil.Getenv("SRC_CONFIG_PATH", "/config/filebeat-output.yml")
	dstConfigPath          = osutil.Getenv("DST_CONFIG_PATH", "/etc/filebeat/filebeat.yml")
	heatlthCheckInterval   = int64(10)
	configCheckInterval    = int64(600)
)

func init() {
	sec, err := strconv.ParseInt(osutil.Getenv(HeatlthCheckInterval,
		strconv.FormatInt(heatlthCheckInterval, 10)), 10, 64)
	if err != nil || sec < 0 {
		log.Warningf("%s is Invalid, use default value %d", HeatlthCheckInterval, heatlthCheckInterval)
	} else {
		heatlthCheckInterval = sec
	}

	sec, err = strconv.ParseInt(osutil.Getenv(ConfigCheckInterval,
		strconv.FormatInt(configCheckInterval, 10)), 10, 64)
	if err != nil || sec < 0 {
		log.Warningf("%s is Invalid, use default value %d", ConfigCheckInterval, configCheckInterval)
	} else {
		configCheckInterval = sec
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return string(h.Sum(nil)), nil
}

func newFileChecker(path string, notify func()) func() {
	var (
		curHash string
		mtx     sync.Mutex
		err     error
	)

	curHash, err = hashFile(path)
	if err != nil {
		log.Warningln(err)
	}

	return func() {
		mtx.Lock()
		defer mtx.Unlock()

		h, err := hashFile(path)
		if err != nil {
			log.Warningln(err)
			return
		}

		if curHash != h {
			log.Infof("file need reload, old: %x, new: %x", curHash, h)
			curHash = h
			notify()
		}
	}
}

func watchFileChange(path string, reloadCh chan<- struct{}) {
	checker := newFileChecker(path, func() { reloadCh <- struct{}{} })

	//watch CM
	go watchConfigMapUpdate(filepath.Dir(path), checker)

	//定时监测
	go func(checkFile func()) {
		check := time.Tick(time.Duration(configCheckInterval) * time.Second)
		for range check {
			checkFile()
		}
	}(checker)
}

func run(stopCh <-chan struct{}) error {
	reloadCh := make(chan struct{}, 1)
	started := false
	cmd := newCmd()

	watchFileChange(srcConfigPath, reloadCh)

	if err := applyChange(); err == nil {
		reloadCh <- struct{}{}
	} else {
		log.Errorf("Error generate config file: %v", err)
		log.Infoln("Filebeat will not start until configmap being updated")
	}

	check := time.Tick(time.Duration(heatlthCheckInterval) * time.Second)
	for {
		select {
		case <-stopCh:
			log.Infoln("Wait filebeat shutdown")
			if err := cmd.Stop(); err != nil {
				return fmt.Errorf("filebeat quit with error: %v", err)
			}
			return nil
		case <-reloadCh:
			log.Infoln("Reload")
			if err := applyChange(); err != nil {
				log.Errorln("Error apply change:", err)
				continue
			}

			if !started {
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("error run filebeat: %v", err)
				}
				log.Infoln("Filebeat start")
				started = true
			} else {
				if err := cmd.Stop(); err != nil {
					return fmt.Errorf("filebeat quit with error: %v", err)
				}
				log.Infoln("Filebeat quit")

				cmd = newCmd()
				if err := cmd.Start(); err != nil {
					return fmt.Errorf("error run filebeat: %v", err)
				}
			}
		case <-check:
			if started {
				if cmd != nil && cmd.Exited() {
					log.Fatalln("Filebeat has unexpectedly exited")
					os.Exit(1)
				}
			}
		}
	}
}

func applyChange() error {
	outputCfgData, err := ioutil.ReadFile(srcConfigPath)
	if err != nil {
		return err
	}

	tmplData, err := ioutil.ReadFile("/etc/filebeat/filebeat.yml.tpl")
	if err != nil {
		return err
	}

	cfg := map[string]interface{}{}
	if err := yaml.Unmarshal(outputCfgData, &cfg); err != nil {
		return fmt.Errorf("error decode output config yaml: %v", err)
	}

	t, err := template.New("filebeat").Parse(string(tmplData))
	if err != nil {
		return err
	}

	f, err := os.OpenFile(dstConfigPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := t.Execute(f, cfg); err != nil {
		return fmt.Errorf("error rendor template: %v", err)
	}

	generated, _ := ioutil.ReadFile(dstConfigPath)
	fmt.Println(string(generated))

	return nil
}

var (
	fbArgs []string
)

func newCmd() *AsyncCmd {
	log.Infof("Will run filebeat with command: %v %v", filebeatExecutablePath, fbArgs)
	cmd := exec.Command(filebeatExecutablePath, fbArgs...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	return WrapCmd(cmd)
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
