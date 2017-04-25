// Package scp provides a simple interface to copying files over a
// go.crypto/ssh session.
package scp

import (
	"fmt"
	"io"
	"os"
	"path"

	"golang.org/x/crypto/ssh"
)

// Copy send data reader through ssh
func Copy(size int64, mode os.FileMode, fileName string, contents io.Reader, destination string, session *ssh.Session) error {
	return copy(size, mode, fileName, contents, destination, session, "")
}

// CopyPath send file through ssh session
func CopyPath(filePath, destinationPath string, session *ssh.Session) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := f.Stat()
	if err != nil {
		return err
	}
	return Copy(s.Size(), s.Mode().Perm(), path.Base(filePath), f, destinationPath, session)
}

func copy(size int64, mode os.FileMode, fileName string, contents io.Reader, destination string, session *ssh.Session, flags string) error {
	defer session.Close()
	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%#o %d %s\n", mode, size, fileName)
		io.Copy(w, contents)
		fmt.Fprint(w, "\x00")
	}()
	cmd := fmt.Sprintf("scp %s -t %s", flags, destination)
	if err := session.Run(cmd); err != nil {
		return err
	}
	return nil
}
