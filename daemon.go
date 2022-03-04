package main

import (
	"context"
	"errors"
	"io"
	"log"
	"lucrum/config"
	"lucrum/lib"
	"lucrum/websocket"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/sevlyar/go-daemon"
)

// Creates a daemon that runs the helper function supplied
func daemonize(
	ctx context.Context,
	wg *sync.WaitGroup,
	daemonConf config.Daemon,
	helper func(context.Context, *sync.WaitGroup, config.Websocket, *os.Process),
) {

	// Create pid file path
	dir, _ := filepath.Split(daemonConf.PidFile)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		log.Fatalln(err)
	}

	// Create log file path
	dir, _ = filepath.Split(daemonConf.LogFile)
	err = os.MkdirAll(dir, 0755)
	lib.Check(err)

	// Get workdir
	cwd, err := os.Getwd()
	lib.Check(err)

	pidPerms, err := strconv.ParseUint(daemonConf.PidFilePerms, 8, 32)
	lib.Check(err)

	logPerms, err := strconv.ParseUint(daemonConf.LogFilePerms, 8, 32)
	lib.Check(err)

	// Create a new daemon context
	daemonCtx := daemon.Context{
		PidFileName: daemonConf.PidFile,
		PidFilePerm: os.FileMode(pidPerms),
		LogFileName: daemonConf.LogFile,
		LogFilePerm: os.FileMode(logPerms),
		WorkDir:     cwd,
		Args:        os.Args,
		Umask:       027,
	}

	// See if this process already exists
	proc, err := daemonCtx.Search()
	// Something bad happened we don't know
	// This catches the file not existing and there not being a PID in the file
	if !os.IsNotExist(err) && !errors.Is(err, io.EOF) && err != nil {
		log.Fatalln(err)
	}

	// A process supposedly exists
	if proc != nil {
		// Check to see if it really exists, and if it does we are done
		if err = proc.Signal(syscall.Signal(0)); err == nil {
			return
		}
	}

	// Daemonize
	child, err := daemonCtx.Reborn()
	lib.Check(err)

	// Run on daemonize
	wsConf := ctx.Value(lib.LucrumKey("conf")).(config.Configuration).Type.Ws
	helper(ctx, wg, wsConf, child)

	// Release the daemon
	err = daemonCtx.Release()
	lib.Check(err)
}

func wsDaemonHelper(ctx context.Context, wg *sync.WaitGroup, conf config.Websocket, child *os.Process) {
	// This is the child
	if child == nil {
		// Call the dispatcher
		websocket.WSDispatcher(ctx, wg, conf.Channels)
	}
	// On the parent we just return, because more work might need to be done
}
