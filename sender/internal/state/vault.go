package state

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

var errInvalidIdentity = errors.New("invalid age X25519 identity")

type Vault struct {
	id *age.X25519Identity
}

func GenerateIdentity() (identity, recipient string, err error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", "", fmt.Errorf("generate identity: %w", err)
	}
	return id.String(), id.Recipient().String(), nil
}

func NewVault(identity string) (*Vault, error) {
	id, err := age.ParseX25519Identity(strings.TrimSpace(identity))
	if err != nil {
		return nil, errInvalidIdentity
	}
	return &Vault{id: id}, nil
}

func (v *Vault) Recipient() string {
	return v.id.Recipient().String()
}

func (v *Vault) Seal(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	defer in.Close()
	err = writeAtomic(dst, func(w io.Writer) error {
		enc, err := age.Encrypt(w, v.id.Recipient())
		if err != nil {
			return err
		}
		if _, err := io.Copy(enc, in); err != nil {
			return err
		}
		return enc.Close()
	})
	if err != nil {
		return fmt.Errorf("seal %s: %w", dst, err)
	}
	return nil
}

func (v *Vault) Unseal(dst, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	defer in.Close()
	err = writeAtomic(dst, func(w io.Writer) error {
		dec, err := age.Decrypt(in, v.id)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, dec)
		return err
	})
	if err != nil {
		return fmt.Errorf("unseal %s: %w", src, err)
	}
	return nil
}
