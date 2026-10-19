package app

import (
	"os"
	"sync"
)

// Keep at most two 1 MiB log files, independent of NAS uptime.
type rotatingLog struct {
	mu   sync.Mutex
	path string
}

func (l *rotatingLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	original := len(b)
	if len(b) > 1024*1024 {
		b = b[len(b)-1024*1024:]
	}
	if st, e := os.Stat(l.path); e == nil && st.Size()+int64(len(b)) > 1024*1024 {
		if e = os.Rename(l.path, l.path+".1"); e != nil {
			return 0, e
		}
	}
	f, e := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if e != nil {
		return 0, e
	}
	_, e = f.Write(b)
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return 0, e
	}
	return original, nil
}
