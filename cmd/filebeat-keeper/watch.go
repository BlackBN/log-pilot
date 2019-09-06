package main

import (
	"path/filepath"

	"github.com/caicloud/nirvana/log"
	"gopkg.in/fsnotify/fsnotify.v1"
)

const (
	dataDirName = "..data"
)

// ref_link [https://github.com/jimmidyson/configmap-reload/issues/6#issuecomment-355203620]
// ConfigMap volumes use an atomic writer. You could familarize yourself with
// the mechanic how atomic writes are implemented. In the end you could check
// if the actual change you do in your ConfigMap results in the rename of the
// ..data-symlink (step 9).
// ref_link [https://github.com/kubernetes/kubernetes/blob/6d98cdbbfb055757a9846dee97dafd4177d9a222/pkg/volume/util/atomic_writer.go#L56]
func watchConfigMapUpdate(path string, update func()) error {
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
				if filepath.Base(ev.Name) == dataDirName {
					log.Infoln("Configmap updated")
					update()
				}
			}
		case err := <-w.Errors:
			log.Errorf("Watch error: %v", err)
		}
	}
}
