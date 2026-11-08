package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	waLog "go.mau.fi/whatsmeow/util/log"
	_ "modernc.org/sqlite"
)

// SessionFile is the encrypted whatsmeow store inside a state directory.
const SessionFile = "session.db.age"

const (
	workDirPattern = "cp-reminders-wa-*"
	plainDBName    = "session.db"
	snapshotName   = "snapshot.db"
	sqlitePragmas  = "?_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=journal_mode(DELETE)"
)

var (
	// ErrNoSession is returned when the state directory has no session.
	ErrNoSession = errors.New("no session in state directory; run `wa pair` first")
	// ErrNotPaired is returned when the session holds no paired device.
	ErrNotPaired = errors.New("session is not paired; run `wa pair`")

	errSessionDiscarded = errors.New("session was discarded")
	errSessionCommitted = errors.New("session was already committed")
)

type sessionStatus int

const (
	sessionOpen sessionStatus = iota
	sessionCommitted
	sessionDiscarded
)

type Session struct {
	Container  *sqlstore.Container
	db         *sql.DB
	dir        string
	vault      *Vault
	workDir    string
	status     sessionStatus
	saveFailed bool
}

// SessionExists reports whether dir holds an encrypted session.
func SessionExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, SessionFile))
	return err == nil
}

func CreateSession(ctx context.Context, dir string, v *Vault, log waLog.Logger) (*Session, error) {
	return openWork(ctx, dir, v, log, nil)
}

// OpenSession decrypts the session in dir.
func OpenSession(ctx context.Context, dir string, v *Vault, log waLog.Logger) (*Session, error) {
	sealed := filepath.Join(dir, SessionFile)
	if _, err := os.Stat(sealed); errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoSession
	} else if err != nil {
		return nil, fmt.Errorf("open session: %w", err)
	}
	return openWork(ctx, dir, v, log, func(plain string) error {
		return v.Unseal(plain, sealed)
	})
}

func openWork(ctx context.Context, dir string, v *Vault, log waLog.Logger, restore func(plain string) error) (_ *Session, err error) {
	work, err := os.MkdirTemp("", workDirPattern)
	if err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(work)
		}
	}()
	plain := filepath.Join(work, plainDBName)
	if restore != nil {
		if err := restore(plain); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", plain+sqlitePragmas)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	container := sqlstore.NewWithDB(db, "sqlite3", log)
	if err := container.Upgrade(ctx); err != nil {
		container.Close()
		return nil, fmt.Errorf("upgrade whatsmeow store: %w", err)
	}
	return &Session{Container: container, db: db, dir: dir, vault: v, workDir: work}, nil
}

// PairedDevice returns the session's device, or ErrNotPaired.
func (s *Session) PairedDevice(ctx context.Context) (*store.Device, error) {
	dev, err := s.Container.GetFirstDevice(ctx)
	if err != nil {
		return nil, fmt.Errorf("load device: %w", err)
	}
	if dev.ID == nil {
		return nil, ErrNotPaired
	}
	return dev, nil
}

func (s *Session) Save() error {
	switch s.status {
	case sessionCommitted:
		return errSessionCommitted
	case sessionDiscarded:
		return errSessionDiscarded
	}
	err := s.snapshotAndSeal()
	s.saveFailed = err != nil
	return err
}

func (s *Session) snapshotAndSeal() error {
	snapshot := filepath.Join(s.workDir, snapshotName)
	if err := os.Remove(snapshot); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("save session: %w", err)
	}
	if _, err := s.db.ExecContext(context.Background(), "VACUUM INTO ?", snapshot); err != nil {
		return fmt.Errorf("save session: snapshot: %w", err)
	}
	sealErr := s.vault.Seal(filepath.Join(s.dir, SessionFile), snapshot)
	if err := errors.Join(sealErr, os.Remove(snapshot)); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

func (s *Session) Commit() error {
	switch s.status {
	case sessionCommitted:
		return nil
	case sessionDiscarded:
		return errSessionDiscarded
	}
	saveErr := s.Save()
	s.status = sessionCommitted
	closeErr := s.Container.Close()
	removeErr := os.RemoveAll(s.workDir)
	if err := errors.Join(saveErr, closeErr, removeErr); err != nil {
		return fmt.Errorf("commit session: %w", err)
	}
	return nil
}

func (s *Session) Release() error {
	if s.status != sessionOpen {
		return nil
	}
	var saveErr error
	if s.saveFailed {
		saveErr = s.Save()
	}
	return errors.Join(saveErr, s.Discard())
}

func (s *Session) Discard() error {
	if s.status != sessionOpen {
		return nil
	}
	s.status = sessionDiscarded
	if err := errors.Join(s.Container.Close(), os.RemoveAll(s.workDir)); err != nil {
		return fmt.Errorf("discard session: %w", err)
	}
	return nil
}
