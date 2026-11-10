package steam

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// follower reads the lines Steam appends to its game log, starting from
// wherever the file ended when the follower was created: the history is
// already over, and games running now are found from the process list.
//
// Steam has always appended to the same file, across restarts and switches
// between Game Mode and Desktop Mode. If the file is ever replaced or cut
// short, the follower starts again at its beginning.
type follower struct {
	path    string
	file    *os.File
	offset  int64
	partial string // the start of a line whose end hasn't been written yet
}

func newFollower(path string) *follower {
	f := &follower{path: path}
	if file, err := os.Open(path); err == nil {
		if info, err := file.Stat(); err == nil {
			f.file, f.offset = file, info.Size()
		} else {
			file.Close()
		}
	}
	return f
}

// read returns the complete lines written since the last read.
func (f *follower) read() ([]string, error) {
	info, err := os.Stat(f.path)
	if os.IsNotExist(err) {
		f.close()
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checking %s: %w", f.path, err)
	}

	var opened os.FileInfo
	if f.file != nil {
		opened, _ = f.file.Stat()
	}
	switch {
	case f.file == nil || opened == nil || !os.SameFile(opened, info):
		// New since the last read, or replaced: everything in it is new.
		f.close()
		file, err := os.Open(f.path)
		if err != nil {
			return nil, fmt.Errorf("opening %s: %w", f.path, err)
		}
		f.file = file
	case info.Size() < f.offset:
		f.offset, f.partial = 0, "" // cut short: start again at the top
	}

	if _, err := f.file.Seek(f.offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.path, err)
	}
	data, err := io.ReadAll(f.file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.path, err)
	}
	f.offset += int64(len(data))
	if len(data) == 0 {
		return nil, nil
	}

	parts := strings.Split(f.partial+string(data), "\n")
	f.partial = parts[len(parts)-1]
	return parts[:len(parts)-1], nil
}

func (f *follower) close() {
	if f.file != nil {
		f.file.Close()
	}
	f.file, f.offset, f.partial = nil, 0, ""
}
