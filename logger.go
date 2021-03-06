package accesslog

import (
	"bytes"
	"fmt"
	"github.com/golang/glog"
	"os"
	"path/filepath"
	"time"

	"github.com/enriquebris/goconcurrentqueue"
)

// the max log size
const MaxLogFileSize int64 = 1024 * 1024 * 1800

type logger interface {
	Log(buf *bytes.Buffer) error
	Close() error
	QueueBufferSize() int
}

type asyncFileLogger struct {
	filename string
	file     *os.File
	queue    goconcurrentqueue.Queue
	close    chan struct{}
	sizeNum  int64
}

func newAsyncFileLogger(cfg *Conf) (logger, error) {
	dir, _ := filepath.Split(cfg.Filename)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return nil, err
	}

	f, err := openAppendFile(cfg.Filename)
	if err != nil {
		return nil, err
	}

	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}

	ret := &asyncFileLogger{
		filename: cfg.Filename,
		file:     f,
		queue:    goconcurrentqueue.NewFIFO(),
		close:    make(chan struct{}),
		sizeNum:  stat.Size(),
	}

	go ret.loop()

	return ret, nil
}

func openAppendFile(fileName string) (*os.File, error) {
	return os.OpenFile(fileName, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeAppend|0666)
}

func (l *asyncFileLogger) Log(buf *bytes.Buffer) error {
	return l.queue.Enqueue(buf)
}

func (l *asyncFileLogger) loop() {
	for {
		select {
		case <-l.close:
			return
		default:
		}

		buf, err := l.queue.Dequeue()
		if err != nil {
			time.Sleep(time.Millisecond * 10)
			continue
		}
		l.writeFile(buf.(*bytes.Buffer))
	}
}

func (l *asyncFileLogger) writeFile(buf *bytes.Buffer) {
	if int64(buf.Len())+l.sizeNum > MaxLogFileSize {
		l.rotateLog()
	}

	n, err := l.file.Write(buf.Bytes())
	logBufPool.Put(buf)
	if err != nil {
		panic("cannot write access log")
	}

	l.sizeNum += int64(n)
}

func (l *asyncFileLogger) rotateLog() {
	l.file.Sync()
	l.file.Close()
	err := os.Rename(l.filename, fmt.Sprintf("%s-%s", l.filename, time.Now().Format("20060102150405")))
	if err != nil {
		panic("fail to rotate log")
	}

	l.file, err = openAppendFile(l.filename)
	if err != nil {
		panic(err)
	}

	stat, err := l.file.Stat()
	if err != nil {
		panic(err)
	}

	l.sizeNum = stat.Size()
}

func (l *asyncFileLogger) QueueBufferSize() int {
	return l.queue.GetLen()
}

func (l *asyncFileLogger) Close() error {
	l.close <- struct{}{}

	for {
		buf, err := l.queue.Dequeue()
		if err != nil {
			goto Done
		}
		l.writeFile(buf.(*bytes.Buffer))
	}

Done:
	l.file.Sync()
	return l.file.Close()
}

// glog
type GlogLogger struct{}

func newGlogLogger() logger {
	return GlogLogger{}
}

func (g GlogLogger) Log(buf *bytes.Buffer) error {
	glog.Info(buf.String())
	return nil
}

func (g GlogLogger) Close() error {
	glog.Flush()
	return nil
}

func (g GlogLogger) QueueBufferSize() int {
	return 0
}
