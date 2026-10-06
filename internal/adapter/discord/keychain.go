package discord

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Keychain は macOS のログインキーチェーンに、利用者のアカウント名で読み書きする。
type Keychain struct {
	Executable string // 空なら /usr/bin/security
}

func (k Keychain) security() string {
	if k.Executable != "" {
		return k.Executable
	}
	return "/usr/bin/security"
}

func account() string {
	if user := os.Getenv("USER"); user != "" {
		return user
	}
	return strconv.Itoa(os.Getuid())
}

func (k Keychain) Get(service string) (string, error) {
	out, err := exec.Command(k.security(), "find-generic-password", "-s", service, "-a", account(), "-w").Output()
	if err != nil {
		return "", fmt.Errorf("キーチェーンに%sがありません", service)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Set は値をコマンドの引数に載せないよう、security -i の標準入力で渡す。
func (k Keychain) Set(service, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return errors.New("キーチェーンに保存する値に改行は使えません")
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", quote(service), quote(account()), quote(value))
	cmd := exec.Command(k.security(), "-i")
	cmd.Stdin = strings.NewReader(line)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("キーチェーンに%sを保存できません", service)
	}
	// security -i は失敗しても終了コードが0のことがあるので、読み返して確かめる
	if got, err := k.Get(service); err != nil || got != value {
		return fmt.Errorf("キーチェーンに%sを保存できません", service)
	}
	return nil
}

// quote は security -i のコマンド行で、1つの引数として扱われるように囲む。
func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
