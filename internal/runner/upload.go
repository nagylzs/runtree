package runner

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/inhies/go-bytesize"
)

// The managed ssh runners (ssh://host without a path) start the rtrunner that runtree uploaded to the host, see
// startScript. When it is not uploaded yet, runtree uploads it once for each host (also when several nodes start at
// the same time), and starts it again.

// EventUpload is the event of an upload (the event attribute of the log record)
const EventUpload = "runner-upload"

// notUploadedError is the error of a start when the binary is not uploaded yet
type notUploadedError struct {
	platform string
}

func (e *notUploadedError) Error() string {
	return "rtrunner is not uploaded yet for " + e.platform
}

// uploads remembers the successful uploads, and the ones in progress, by destination and hash
var uploads = struct {
	lock sync.Mutex
	m    map[string]*uploadCall
}{m: make(map[string]*uploadCall)}

type uploadCall struct {
	done chan struct{}
	err  error
}

// uploadOnce calls upload, unless the same binary was uploaded to the same destination already (by this runtree). The
// calls at the same time wait for the one that uploads.
func uploadOnce(key string, upload func() error) error {
	uploads.lock.Lock()
	c, ok := uploads.m[key]
	if !ok {
		c = &uploadCall{done: make(chan struct{})}
		uploads.m[key] = c
	}
	uploads.lock.Unlock()
	if ok {
		<-c.done
		return c.err
	}
	c.err = upload()
	if c.err != nil {
		// a later start can try again
		uploads.lock.Lock()
		delete(uploads.m, key)
		uploads.lock.Unlock()
	}
	close(c.done)
	return c.err
}

// dialManaged starts the uploaded rtrunner on the host, and uploads it first if needed
func dialManaged(a Address, logger *slog.Logger, handshake func(*sshConn) (*Client, error)) (*Client, error) {
	bins, err := loadBinaries()
	if err != nil {
		return nil, err
	}
	if len(bins) == 0 {
		return nil, errNoBinaries
	}
	command := "sh -c '" + startScript(bins) + "'"
	for attempt := 0; ; attempt++ {
		conn, err := dialSSH(a, command, logger)
		if err != nil {
			return nil, err
		}
		c, err := handshake(conn)
		var notUploaded *notUploadedError
		if err == nil || attempt > 0 || !errors.As(err, &notUploaded) {
			return c, err
		}
		b := bins[notUploaded.platform]
		if b == nil {
			return nil, err // the script has the platforms of bins only
		}
		key := sshDest(a) + ":" + a.Port + " " + b.hash
		if err := uploadOnce(key, func() error { return upload(a, b, logger) }); err != nil {
			return nil, err
		}
	}
}

// upload uploads the binary to the host
func upload(a Address, b *runnerBinary, logger *slog.Logger) error {
	content, err := b.load()
	if err != nil {
		return err
	}
	began := time.Now()
	args := append(sshCommand[1:len(sshCommand):len(sshCommand)], sshOptions(a)...)
	args = append(args, "-o", "Compression=yes", "--", sshDest(a), "sh -c '"+uploadScript(b.hash)+"'")
	cmd := sshCmd(sshCommand[0], args)
	cmd.Stdin = bytes.NewReader(content)
	output := &tailWriter{max: 4096, logger: logger}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Run(); err != nil {
		msg := output.lastLines(3)
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("could not upload rtrunner %s to %s: %s", b.platform, sshDest(a), msg)
	}
	elapsed := time.Since(began)
	logger.Info(fmt.Sprintf("uploaded rtrunner %s to %s (%s in %s)", b.platform, sshDest(a),
		bytesize.New(float64(b.size)), elapsed.Round(10*time.Millisecond)),
		"event", EventUpload, "platform", b.platform, "sha256", b.hash, "bytes", b.size, "elapsed", elapsed)
	return nil
}

// notUploaded returns the error of a start whose script exited with exitNotUploaded, from its standard error
func notUploaded(stderr string) error {
	for _, line := range strings.Split(stderr, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), notUploadedMessage); ok {
			return &notUploadedError{platform: p}
		}
	}
	return nil
}
