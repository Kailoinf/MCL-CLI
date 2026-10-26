// Package secret encrypts account tokens with a key derived from this machine.
//
// Design: AES-256-GCM, key = PBKDF2-SHA256(machine identity, salt). The salt
// lives in state.toml, the identity is read from the host (/etc/machine-id,
// product UUID, hostname, user). Consequences, on purpose:
//
//   - no OS keychain / secret service dependency
//   - copying state.toml to another machine makes the tokens undecryptable
//   - stealing state.toml alone is not enough to read the refresh token
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"

	"github.com/Kailoinf/MCL-CLI/internal/i18n"
)

// KDFIterations is deliberately slow-ish: key derivation happens once per
// command, so ~0.2s is an acceptable price.
const KDFIterations = 200_000

// MachineIdentity collects stable, host-only identifiers.
func MachineIdentity() string {
	var parts []string
	parts = append(parts, "os="+runtime.GOOS)
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id", "/sys/class/dmi/id/product_uuid"} {
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				parts = append(parts, p+"="+s)
			}
		}
	}
	if h, err := os.Hostname(); err == nil {
		parts = append(parts, "host="+h)
	}
	if u, err := user.Current(); err == nil {
		parts = append(parts, "user="+u.Uid+"."+u.Username)
	}
	return strings.Join(parts, "|")
}

// DeriveKey stretches the machine identity + salt into a 32-byte AES key.
func DeriveKey(identity string, salt []byte) ([]byte, error) {
	key, err := pbkdf2.Key(sha256.New, identity, salt, KDFIterations, 32)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T("err.secret.derive", err))
	}
	return key, nil
}

// NewSalt returns fresh salt bytes for a brand-new account (also doubles as a
// "this DB was created here" marker).
func NewSalt() ([]byte, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Encrypt seals plaintext and returns base64(nonce || ciphertext).
func Encrypt(key, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt.
func Decrypt(key []byte, encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, errors.New(i18n.T("err.secret.short"))
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	out, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New(i18n.T("err.secret.decrypt"))
	}
	return out, nil
}
