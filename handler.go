package scp

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// ErrTimes error descibe scp fail
type ErrTimes struct {
	err   error
	times int
}

func (err ErrTimes) Error() string {
	return fmt.Sprintf("copy fail after try %d times: %s", err.times, err.err.Error())
}

type ScpHelper interface {
	Copy(io.Reader, int64, string) error
	CopyPath(string, string) error
	MustCopy(io.Reader, int64, string)
	MustCopyPath(string, string)
	TryCopy(io.Reader, int64, string, int) error
	TryCopyPath(string, string, int) error

	SetLimitKB(int)
	SetGzipEnable(bool)
}

type SshDialer struct {
	SSHUser string
	SSHFile string
	SSHPass string
	SSHAddr string
}

func (d SshDialer) Dial() (*ssh.Client, error) {
	var authm ssh.AuthMethod
	if d.SSHFile != "" {
		b, err := ioutil.ReadFile(d.SSHFile)
		if err != nil {
			return nil, err
		}

		key, err := ssh.ParsePrivateKey(b)
		if err != nil {
			return nil, err
		}

		authm = ssh.PublicKeys(key)
	} else {
		authm = ssh.Password(d.SSHPass)
	}

	return ssh.Dial("tcp", d.SSHAddr, &ssh.ClientConfig{
		Auth: []ssh.AuthMethod{authm},
		User: d.SSHUser,
	})
}

type scpHelperDelegate struct {
	dialer *SshDialer
	client *ssh.Client
	lock   sync.RWMutex
	flags  string
	gzip   bool
}

// NewScpHelper New Scp Helper
func NewScpHelper(dialer *SshDialer) ScpHelper {
	return &scpHelperDelegate{dialer: dialer}
}

func (s *scpHelperDelegate) newSession() (*ssh.Session, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	var err error
	if s.client == nil {
		if s.client, err = s.dialer.Dial(); err != nil {
			return nil, err
		}
	}

	if sess, err := s.client.NewSession(); err != nil {
		s.client.Close()
		s.client = nil
	} else {
		return sess, nil
	}

	if s.client, err = s.dialer.Dial(); err != nil {
		return nil, err
	}

	return s.client.NewSession()
}

func (s *scpHelperDelegate) Copy(r io.Reader, size int64, dstfile string) error {
	session, err := s.newSession()
	if err != nil {
		return err
	}

	defer session.Close()

	name := filepath.Base(dstfile)
	path := filepath.Dir(dstfile)

	if s.gzip {
		name = name + ".gz"
		cb := bytes.NewBuffer(nil)
		w := gzip.NewWriter(cb)
		b, err := ioutil.ReadAll(r)
		if err != nil {
			return err
		}

		if _, err = w.Write(b); err != nil {
			return err
		}
		r = cb
		size = int64(cb.Len())
	}
	return copy(size, os.ModePerm, name, r, path, session, s.flags)
}

func (s *scpHelperDelegate) MustCopy(r io.Reader, size int64, dstfile string) {
	retryTimes := 0

	for {
		if retryTimes > 10 {
			time.Sleep(time.Minute)
		} else if retryTimes > 0 {
			time.Sleep(time.Duration(retryTimes) * time.Second)
		}
		retryTimes++
		if err := s.Copy(r, size, dstfile); err == nil {
			return
		}
	}
}

func (s *scpHelperDelegate) TryCopy(r io.Reader, size int64, dstfile string, trys int) error {
	retryTimes := 0
	var err error

	for {
		if retryTimes > trys {
			return &ErrTimes{times: retryTimes, err: err}
		} else if retryTimes > 0 {
			time.Sleep(time.Duration(retryTimes) * time.Second)
		}
		retryTimes++
		if err := s.Copy(r, size, dstfile); err == nil {
			return nil
		}
	}
}

func (s scpHelperDelegate) openFile(filename string) (io.ReadCloser, int64, error) {
	fd, err := os.Open(filename)
	if err != nil {
		return nil, 0, err
	}

	stat, err := fd.Stat()
	if err != nil {
		return nil, 0, err
	}
	return fd, stat.Size(), nil
}

func (s *scpHelperDelegate) CopyPath(srcfile, dstfile string) error {
	if fd, size, err := s.openFile(srcfile); err == nil {
		return s.Copy(fd, size, dstfile)
	} else {
		return err
	}
}

func (s *scpHelperDelegate) MustCopyPath(srcfile, dstfile string) {
	if fd, size, err := s.openFile(srcfile); err != nil {
		panic(err)
	} else {
		s.MustCopy(fd, size, dstfile)
	}
}

func (s *scpHelperDelegate) TryCopyPath(srcfile, dstfile string, trys int) error {
	if fd, size, err := s.openFile(srcfile); err == nil {
		return s.TryCopy(fd, size, dstfile, trys)
	} else {
		return err
	}
}

func (s *scpHelperDelegate) SetLimitKB(kbs int) {
	s.flags = fmt.Sprintf("-l %d", kbs*8)
}

func (s *scpHelperDelegate) SetGzipEnable(enable bool) {
	s.gzip = enable
}
