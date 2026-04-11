// Orphan helper — used by TestCodexRPC_NoOrphanOnKill9.
// Takes argv[1] = codex shim path, argv[2] = pidfile path.
// Creates a CodexBackend pointed at the shim, calls Start(), writes its
// own PID to the pidfile, and blocks forever. The test kill -9's this
// process and asserts the shim did not survive.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/TejGandham/roundtable/internal/roundtable"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: orphan-helper <codex-shim> <pidfile>")
		os.Exit(2)
	}
	shim := os.Args[1]
	pidFile := os.Args[2]

	cb := roundtable.NewCodexBackend(shim, "")
	if err := cb.Start(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "Start:", err)
		os.Exit(1)
	}

	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "WriteFile:", err)
		os.Exit(1)
	}

	// Block forever; test will SIGKILL us.
	for {
		time.Sleep(1 * time.Second)
	}
}
