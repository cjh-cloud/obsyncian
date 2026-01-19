package main

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches a directory for changes and triggers callbacks with debouncing
type FileWatcher struct {
	watcher      *fsnotify.Watcher
	watchPath    string
	debounceTime time.Duration
	onChange     func()
	stopChan     chan struct{}
	mu           sync.Mutex
	timer        *time.Timer
}

// NewFileWatcher creates a new file watcher for the given path
func NewFileWatcher(path string, debounceTime time.Duration, onChange func()) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher:      watcher,
		watchPath:    path,
		debounceTime: debounceTime,
		onChange:     onChange,
		stopChan:     make(chan struct{}),
	}

	// Add the root path and all subdirectories
	if err := fw.addRecursive(path); err != nil {
		watcher.Close()
		return nil, err
	}

	return fw, nil
}

// addRecursive adds the given path and all subdirectories to the watcher
func (fw *FileWatcher) addRecursive(path string) error {
	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden directories (like .git, .obsidian)
			if len(info.Name()) > 1 && info.Name()[0] == '.' {
				return filepath.SkipDir
			}
			return fw.watcher.Add(walkPath)
		}
		return nil
	})
}

// Start begins watching for file changes
func (fw *FileWatcher) Start() {
	go fw.watch()
}

// watch is the main event loop for the file watcher
func (fw *FileWatcher) watch() {
	for {
		select {
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Handle new directories being created - add them to watch list
			if event.Op&fsnotify.Create == fsnotify.Create {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					// Skip hidden directories
					if len(info.Name()) > 1 && info.Name()[0] != '.' {
						fw.watcher.Add(event.Name)
					}
				}
			}

			// Trigger debounced callback for write/create/remove/rename events
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
				fw.triggerDebounced()
			}

		case _, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			// Log errors but don't stop watching
			// Could add error callback here if needed

		case <-fw.stopChan:
			return
		}
	}
}

// triggerDebounced resets the debounce timer and triggers the callback after the debounce period
func (fw *FileWatcher) triggerDebounced() {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Cancel any existing timer
	if fw.timer != nil {
		fw.timer.Stop()
	}

	// Set a new timer
	fw.timer = time.AfterFunc(fw.debounceTime, func() {
		if fw.onChange != nil {
			fw.onChange()
		}
	})
}

// Stop stops the file watcher
func (fw *FileWatcher) Stop() {
	close(fw.stopChan)
	fw.watcher.Close()

	fw.mu.Lock()
	if fw.timer != nil {
		fw.timer.Stop()
	}
	fw.mu.Unlock()
}
