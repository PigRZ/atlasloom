// 本文件封装密码哈希与校验，避免认证处理器接触密码学细节。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
)

func HashPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", fmt.Errorf("密码至少需要 6 个字符")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return strings.Join([]string{"argon2id", "v=19", fmt.Sprintf("m=%d,t=%d,p=%d", argonMemory, argonTime, argonThreads), base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)}, "$"), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" {
		return false
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[3])
	want, keyErr := base64.RawStdEncoding.DecodeString(parts[4])
	if saltErr != nil || keyErr != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, uint32(len(want)))
	return subtle.ConstantTimeCompare(actual, want) == 1
}

func RandomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
