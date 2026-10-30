package check

import (
	"errors"
	"os/exec"
	"strings"
)

func CheckLinkArgs(cmdArgs []string) error {
	cmd := exec.Command("clang")
	cmd.Args = append(cmd.Args, cmdArgs...)
	cmd.Args = append(cmd.Args, "-x", "c", "-o", "/dev/null", "-")
	src := "int main() {return 0;}"
	srcIn := strings.NewReader(src)
	cmd.Stdin = srcIn
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(string(out))
	}
	return nil
}
