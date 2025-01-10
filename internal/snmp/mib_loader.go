package snmp

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/sleepinggenius2/gosmi"

	"github.com/influxdata/telegraf"
)

// must init, append path for each directory, load module for every file
// or gosmi will fail without saying why
var m sync.Mutex
var once sync.Once
var cache = make(map[string]bool)

type MibLoader interface {
	// appendPath takes the path of a directory
	appendPath(path string)

	// loadModule takes the name of a file in one of the
	// directories. Basename only, no relative or absolute path
	loadModule(path string) error
}

type GosmiMibLoader struct{}

func (*GosmiMibLoader) appendPath(path string) {
	m.Lock()
	defer m.Unlock()

	gosmi.AppendPath(path)
}

func (*GosmiMibLoader) loadModule(path string) error {
	m.Lock()
	defer m.Unlock()

	_, err := gosmi.LoadModule(path)
	return err
}

// will give all found folders to gosmi and load in all modules found in the folders
func LoadMibsFromPath(paths []string, log telegraf.Logger, loader MibLoader) error {
	folders, err := walkPaths(paths, log)
	if err != nil {
		return err
	}
	for _, path := range folders {
		loader.appendPath(path)
		modules, err := os.ReadDir(path)
		if err != nil {
			log.Warnf("Can't read directory %v", modules)
			continue
		}

		for _, entry := range modules {
			info, err := entry.Info()
			if err != nil {
				log.Warnf("Couldn't get info for %v: %v", entry.Name(), err)
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				symlink := filepath.Join(path, info.Name())
				target, err := filepath.EvalSymlinks(symlink)
				if err != nil {
					log.Warnf("Couldn't evaluate symbolic links for %v: %v", symlink, err)
					continue
				}
				// replace symlink's info with the target's info
				info, err = os.Lstat(target)
				if err != nil {
					log.Warnf("Couldn't stat target %v: %v", target, err)
					continue
				}
			}
			if info.Mode().IsRegular() {
				err := loader.loadModule(info.Name())
				if err != nil {
					log.Warnf("Couldn't load module %v: %v", info.Name(), err)
					continue
				}
			}
		}
	}
	return nil
}

// should walk the paths given and find all folders
func walkPaths(paths []string, log telegraf.Logger) ([]string, error) {
	once.Do(gosmi.Init)

	folders := make([]string, 0)
	for _, mibPath := range paths {
		// Check if we loaded that path already and skip it if so
		m.Lock()
		cached := cache[mibPath]
		cache[mibPath] = true
		m.Unlock()
		if cached {
			continue
		}

		err := filepath.Walk(mibPath, func(path string, info os.FileInfo, err error) error {
			if info == nil {
				log.Warnf("No mibs found")
				if os.IsNotExist(err) {
					log.Warnf("MIB path doesn't exist: %q", mibPath)
				} else if err != nil {
					return err
				}
				return nil
			}

			if info.Mode()&os.ModeSymlink != 0 {
				target, err := filepath.EvalSymlinks(path)
				if err != nil {
					log.Warnf("Couldn't evaluate symbolic links for %v: %v", path, err)
				}
				info, err = os.Lstat(target)
				if err != nil {
					log.Warnf("Couldn't stat target %v: %v", target, err)
				}
				path = target
			}
			if info.IsDir() {
				folders = append(folders, path)
			}

			return nil
		})
		if err != nil {
			return folders, fmt.Errorf("couldn't walk path %q: %w", mibPath, err)
		}
	}
	return folders, nil
}
