//go:build owngo && owngomn

// Програма прийняття P-NEW-09.6: взаємоблокування виявляється, як у gc.
// Дочірній процес: головна горутина й ще одна чекають на каналах, у які
// ніхто не пише. Код 0 лише якщо дочірній завершився з кодом 2 і написав
// «fatal error: all goroutines are asleep - deadlock!».
package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		a, b := make(chan int), make(chan int)
		go func() { <-a }()
		time.Sleep(10 * time.Millisecond)
		<-b
		return
	}
	cmd := exec.Command(os.Args[0], "child")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		println("start:", err.Error())
		os.Exit(2)
	}
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(20 * time.Second):
		cmd.Process.Kill()
		println("child hangs instead of reporting a deadlock")
		os.Exit(3)
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 ||
		!bytes.Contains(stderr.Bytes(), []byte("fatal error: all goroutines are asleep - deadlock!")) {
		println("child:", err, "want exit 2 and the deadlock report")
		os.Stderr.Write(stderr.Bytes())
		os.Exit(4)
	}
}
