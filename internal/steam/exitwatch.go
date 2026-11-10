package steam

import (
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

// exitWatch waits for a set of processes to exit, using pidfds: the kernel
// makes a process's pidfd readable the moment it exits, so this sleeps until
// then instead of checking the process list over and over.
//
// It backs up Steam's log. If the Steam client crashes or restarts mid-game,
// nothing in the log says the game ended, but its processes exiting does.
type exitWatch struct {
	cancelRead, cancelWrite int
	stopOnce                sync.Once
}

// watchExits calls exited once every one of pids has exited. Each pid is
// first opened as a pidfd and then checked with still, which guards against
// a PID that was reused by another process in the meantime. It returns a nil
// watch when none of the processes is still there, and an error when the
// kernel can't open pidfds at all (older than Linux 5.3).
func watchExits(pids []int, still func(pid int) bool, exited func()) (*exitWatch, error) {
	var fds []int
	closeAll := func() {
		for _, fd := range fds {
			unix.Close(fd)
		}
	}
	for _, pid := range pids {
		fd, err := unix.PidfdOpen(pid, 0)
		if errors.Is(err, unix.ESRCH) {
			continue // already gone
		}
		if err != nil {
			closeAll()
			return nil, err
		}
		if still != nil && !still(pid) {
			unix.Close(fd)
			continue
		}
		fds = append(fds, fd)
	}
	if len(fds) == 0 {
		return nil, nil
	}

	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
		closeAll()
		return nil, err
	}
	w := &exitWatch{cancelRead: pipe[0], cancelWrite: pipe[1]}
	go w.wait(fds, exited)
	return w, nil
}

func (w *exitWatch) wait(fds []int, exited func()) {
	defer unix.Close(w.cancelRead)
	for len(fds) > 0 {
		polls := []unix.PollFd{{Fd: int32(w.cancelRead), Events: unix.POLLIN}}
		for _, fd := range fds {
			polls = append(polls, unix.PollFd{Fd: int32(fd), Events: unix.POLLIN})
		}
		if _, err := unix.Poll(polls, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			break // can't wait any more: leave it to Steam's log
		}
		if polls[0].Revents != 0 {
			break // stopped
		}
		alive := fds[:0]
		for i, fd := range fds {
			if polls[i+1].Revents != 0 {
				unix.Close(fd)
			} else {
				alive = append(alive, fd)
			}
		}
		fds = alive
	}
	if len(fds) == 0 {
		exited()
		return
	}
	for _, fd := range fds {
		unix.Close(fd)
	}
}

// stop ends the watch without calling exited. It's safe to call more than once,
// and after the processes have exited.
func (w *exitWatch) stop() {
	w.stopOnce.Do(func() { unix.Close(w.cancelWrite) })
}
