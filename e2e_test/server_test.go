// +build integration

package e2e_test

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/vishvananda/netns"

	"github.com/coredhcp/coredhcp"
	"github.com/coredhcp/coredhcp/config"

	// Plugins
	_ "github.com/coredhcp/coredhcp/plugins/server_id"
)

var serverConfig = config.Config{
	Server6: &config.ServerConfig{
		Listener: &net.UDPAddr{
			Port: dhcpv6.DefaultServerPort,
		},
		Plugins: []*config.PluginConfig{
			{Name: "server_id", Args: []string{"LL", "11:22:33:44:55:66"}},
		},
	},
}

// This function *must* be run in its own routine
// For now this assumes ns are created outside.
// TODO: dynamically create NS and interfaces directly in the test program
func runServer(nsName string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ns, err := netns.GetFromName(nsName)
	if err != nil {
		log.Fatal("netns not set up")
	}
	if netns.Set(ns) != nil {
		panic("netns not set up")
	}
	server := coredhcp.NewServer(&serverConfig)
	if err := server.Start(); err != nil {
		panic(fmt.Errorf("Server could not start: %w", err))
	}
	if err := server.Wait(); err != nil {
		panic(fmt.Errorf("Server errored during run: %w", err))
	}
}

// runInNs will execute the provided cmd in the namespace nsName.
// It returns the error status of the cmd. Errors in NS management will panic
func runInNs(nsName string, cmd exec.Cmd) (string, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	backupNS, err := netns.Get()
	if err != nil {
		panic("Could not save handle to original NS")
	}

	ns, err := netns.GetFromName(nsName)
	if err != nil {
		panic("netns not set up")
	}
	if netns.Set(ns) != nil {
		panic("Couldn't switch to test NS")
	}

	out, status := cmd.CombinedOutput()

	if netns.Set(backupNS) != nil {
		panic("couldn't switch back to original NS")
	}

	return string(out), status
}

var clientCommand = exec.Command("/sbin/dhclient",
	"-6", "-d", "-v", "-1", "-lf", "/dev/null", "-pf", "/dev/null",
)

// TestDora creates a server and attempts to connect to it
func TestDora(t *testing.T) {
	go runServer("coredhcp-direct-upper")
	out, err := runInNs("coredhcp-direct-lower", *clientCommand)
	t.Log(clientCommand.String())
	t.Log(out)
	if err != nil {
		t.Error(err)
	}
}
